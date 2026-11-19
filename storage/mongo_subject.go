package storage

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoSubjectRepo struct {
	db *mongo.Database
}

func (r *mongoSubjectRepo) Facts(ctx context.Context, entityID int64) ([]SubjectFact, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()
	cur, err := r.db.Collection("memori_entity_fact").Find(ctx, bson.M{"entity_id": entityID}, options.Find().
		SetProjection(bson.M{"content_embedding": 0, "search_tokens": 0}).
		SetSort(bson.D{{Key: "id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []SubjectFact
	for cur.Next(ctx) {
		var doc struct {
			ID                   int64      `bson:"id"`
			Content              string     `bson:"content"`
			Slot                 string     `bson:"slot"`
			Status               string     `bson:"status"`
			NumTimes             int64      `bson:"num_times"`
			DateLastTime         time.Time  `bson:"date_last_time"`
			ValidFrom            time.Time  `bson:"valid_from"`
			ValidTo              *time.Time `bson:"valid_to"`
			SourceMessageID      *int64     `bson:"source_message_id"`
			SourceConversationID *int64     `bson:"source_conversation_id"`
			Confidence           float64    `bson:"confidence"`
			AssertedBy           string     `bson:"asserted_by"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		fact := SubjectFact{
			ID:                   doc.ID,
			Content:              doc.Content,
			Slot:                 doc.Slot,
			Status:               doc.Status,
			NumTimes:             doc.NumTimes,
			DateLastTime:         doc.DateLastTime,
			ValidFrom:            doc.ValidFrom,
			ValidTo:              doc.ValidTo,
			SourceMessageID:      doc.SourceMessageID,
			SourceConversationID: doc.SourceConversationID,
			Confidence:           doc.Confidence,
			AssertedBy:           doc.AssertedBy,
		}
		if fact.Status == "" {
			fact.Status = "active"
		}
		if fact.Confidence == 0 {
			fact.Confidence = 1
		}
		if fact.AssertedBy == "" {
			fact.AssertedBy = "user"
		}
		out = append(out, fact)
	}
	return out, cur.Err()
}

func (r *mongoSubjectRepo) Messages(ctx context.Context, entityID int64) ([]StoredMessage, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()
	sessionIDs, err := mongoIDs(ctx, r.db.Collection("memori_session"), bson.M{"entity_id": entityID})
	if err != nil || len(sessionIDs) == 0 {
		return nil, err
	}
	conversationIDs, err := mongoIDs(ctx, r.db.Collection("memori_conversation"), bson.M{"session_id": bson.M{"$in": sessionIDs}})
	if err != nil || len(conversationIDs) == 0 {
		return nil, err
	}
	cur, err := r.db.Collection("memori_conversation_message").Find(ctx, bson.M{
		"conversation_id": bson.M{"$in": conversationIDs},
	}, options.Find().SetSort(bson.D{{Key: "id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	return decodeMongoMessages(ctx, cur, len(conversationIDs))
}

func (r *mongoSubjectRepo) Delete(ctx context.Context, entityID int64) (DeletionReport, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()
	var report DeletionReport
	sessionIDs, err := mongoIDs(ctx, r.db.Collection("memori_session"), bson.M{"entity_id": entityID})
	if err != nil {
		return report, err
	}
	var conversationIDs []int64
	if len(sessionIDs) > 0 {
		conversationIDs, err = mongoIDs(ctx, r.db.Collection("memori_conversation"), bson.M{"session_id": bson.M{"$in": sessionIDs}})
		if err != nil {
			return report, err
		}
	}
	if len(conversationIDs) > 0 {
		res, err := r.db.Collection("memori_conversation_message").DeleteMany(ctx, bson.M{"conversation_id": bson.M{"$in": conversationIDs}})
		if err != nil {
			return report, err
		}
		report.Messages = res.DeletedCount
		res, err = r.db.Collection("memori_conversation").DeleteMany(ctx, bson.M{"id": bson.M{"$in": conversationIDs}})
		if err != nil {
			return report, err
		}
		report.Conversations = res.DeletedCount
	}
	if len(sessionIDs) > 0 {
		res, err := r.db.Collection("memori_session").DeleteMany(ctx, bson.M{"id": bson.M{"$in": sessionIDs}})
		if err != nil {
			return report, err
		}
		report.Sessions = res.DeletedCount
	}
	res, err := r.db.Collection("memori_entity_fact").DeleteMany(ctx, bson.M{"entity_id": entityID})
	if err != nil {
		return report, err
	}
	report.Facts = res.DeletedCount
	res, err = r.db.Collection("memori_knowledge_graph").DeleteMany(ctx, bson.M{"entity_id": entityID})
	if err != nil {
		return report, err
	}
	report.GraphEdges = res.DeletedCount
	if err := deleteUnreferenced(ctx, r.db, "memori_subject", "subject_id"); err != nil {
		return report, err
	}
	if err := deleteUnreferenced(ctx, r.db, "memori_predicate", "predicate_id"); err != nil {
		return report, err
	}
	if err := deleteUnreferenced(ctx, r.db, "memori_object", "object_id"); err != nil {
		return report, err
	}
	res, err = r.db.Collection("memori_entity").DeleteMany(ctx, bson.M{"id": entityID})
	if err != nil {
		return report, err
	}
	report.Entities = res.DeletedCount
	return report, nil
}

func deleteUnreferenced(ctx context.Context, db *mongo.Database, coll, field string) error {
	referenced, err := mongoFieldIDs(ctx, db.Collection("memori_knowledge_graph"), field)
	if err != nil {
		return err
	}
	filter := bson.M{}
	if len(referenced) > 0 {
		filter["id"] = bson.M{"$nin": referenced}
	}
	_, err = db.Collection(coll).DeleteMany(ctx, filter)
	return err
}

func mongoFieldIDs(ctx context.Context, coll *mongo.Collection, field string) ([]int64, error) {
	cur, err := coll.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{field: 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	seen := map[int64]struct{}{}
	var ids []int64
	for cur.Next(ctx) {
		raw := bson.M{}
		if err := cur.Decode(&raw); err != nil {
			return nil, err
		}
		id, ok := asInt64(raw[field])
		if !ok || id == 0 {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, cur.Err()
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	default:
		return 0, false
	}
}
