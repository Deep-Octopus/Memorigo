package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Deep-Octopus/Memorigo/textutil"
)

func mongoOpCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, 10*time.Second)
}

func mapMongoNotFound(err error) error {
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}
	return err
}

type mongoEntityRepo struct {
	db *mongo.Database
}

func (r *mongoEntityRepo) Create(ctx context.Context, externalID string) (int64, error) {
	if id, err := r.GetByExternalID(ctx, externalID); err == nil {
		return id, nil
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	seq, err := nextSeq(ctx, r.db, "memori_entity")
	if err != nil {
		return 0, err
	}
	org, app, _ := ScopeParts(externalID)
	doc := bson.M{
		"id":           seq,
		"uuid":         uuid.New().String(),
		"external_id":  externalID,
		"org_id":       org,
		"app_id":       app,
		"date_created": time.Now(),
	}
	_, err = r.db.Collection("memori_entity").InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return r.GetByExternalID(ctx, externalID)
		}
		return 0, err
	}
	return seq, nil
}

func (r *mongoEntityRepo) GetByExternalID(ctx context.Context, externalID string) (int64, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	var doc struct {
		ID int64 `bson:"id"`
	}
	err := r.db.Collection("memori_entity").FindOne(ctx, bson.M{"external_id": externalID}).Decode(&doc)
	if err != nil {
		return 0, mapMongoNotFound(err)
	}
	return doc.ID, nil
}

type mongoProcessRepo struct {
	db *mongo.Database
}

func (r *mongoProcessRepo) Create(ctx context.Context, externalID string) (int64, error) {
	if id, err := r.GetByExternalID(ctx, externalID); err == nil {
		return id, nil
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	seq, err := nextSeq(ctx, r.db, "memori_process")
	if err != nil {
		return 0, err
	}
	doc := bson.M{
		"id":           seq,
		"uuid":         uuid.New().String(),
		"external_id":  externalID,
		"date_created": time.Now(),
	}
	_, err = r.db.Collection("memori_process").InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return r.GetByExternalID(ctx, externalID)
		}
		return 0, err
	}
	return seq, nil
}

func (r *mongoProcessRepo) GetByExternalID(ctx context.Context, externalID string) (int64, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	var doc struct {
		ID int64 `bson:"id"`
	}
	err := r.db.Collection("memori_process").FindOne(ctx, bson.M{"external_id": externalID}).Decode(&doc)
	if err != nil {
		return 0, mapMongoNotFound(err)
	}
	return doc.ID, nil
}

type mongoSessionRepo struct {
	db *mongo.Database
}

func (r *mongoSessionRepo) Create(ctx context.Context, entityID, processID *int64, sessionUUID uuid.UUID) (int64, error) {
	if id, err := r.GetByUUID(ctx, sessionUUID); err == nil {
		return id, nil
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	seq, err := nextSeq(ctx, r.db, "memori_session")
	if err != nil {
		return 0, err
	}
	doc := bson.M{
		"id":           seq,
		"uuid":         sessionUUID.String(),
		"date_created": time.Now(),
	}
	if entityID != nil {
		doc["entity_id"] = *entityID
	}
	if processID != nil {
		doc["process_id"] = *processID
	}
	_, err = r.db.Collection("memori_session").InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return r.GetByUUID(ctx, sessionUUID)
		}
		return 0, err
	}
	return seq, nil
}

func (r *mongoSessionRepo) GetByUUID(ctx context.Context, sessionUUID uuid.UUID) (int64, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	var doc struct {
		ID int64 `bson:"id"`
	}
	err := r.db.Collection("memori_session").FindOne(ctx, bson.M{"uuid": sessionUUID.String()}).Decode(&doc)
	if err != nil {
		return 0, mapMongoNotFound(err)
	}
	return doc.ID, nil
}

type mongoConversationRepo struct {
	db *mongo.Database
}

func (r *mongoConversationRepo) Create(ctx context.Context, sessionID int64) (int64, error) {
	if id, err := r.GetBySessionID(ctx, sessionID); err == nil {
		return id, nil
	} else if !errors.Is(err, ErrNotFound) {
		return 0, err
	}

	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	seq, err := nextSeq(ctx, r.db, "memori_conversation")
	if err != nil {
		return 0, err
	}
	doc := bson.M{
		"id":           seq,
		"uuid":         uuid.New().String(),
		"session_id":   sessionID,
		"date_created": time.Now(),
	}
	_, err = r.db.Collection("memori_conversation").InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return r.GetBySessionID(ctx, sessionID)
		}
		return 0, err
	}
	return seq, nil
}

func (r *mongoConversationRepo) GetBySessionID(ctx context.Context, sessionID int64) (int64, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	var doc struct {
		ID int64 `bson:"id"`
	}
	err := r.db.Collection("memori_conversation").FindOne(ctx, bson.M{"session_id": sessionID}).Decode(&doc)
	if err != nil {
		return 0, mapMongoNotFound(err)
	}
	return doc.ID, nil
}

func (r *mongoConversationRepo) UpdateSummary(ctx context.Context, conversationID int64, summary string) error {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	_, err := r.db.Collection("memori_conversation").UpdateOne(
		ctx,
		bson.M{"id": conversationID},
		bson.M{"$set": bson.M{"summary": summary, "date_updated": time.Now()}},
	)
	return err
}

func (r *mongoConversationRepo) GetSummary(ctx context.Context, conversationID int64) (string, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	var doc struct {
		Summary string `bson:"summary"`
	}
	err := r.db.Collection("memori_conversation").FindOne(ctx, bson.M{"id": conversationID}).Decode(&doc)
	if err != nil {
		return "", mapMongoNotFound(err)
	}
	return doc.Summary, nil
}

type mongoMessageRepo struct {
	db *mongo.Database
}

func (r *mongoMessageRepo) Create(ctx context.Context, conversationID int64, role, msgType, content string) error {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	seq, err := nextSeq(ctx, r.db, "memori_conversation_message")
	if err != nil {
		return err
	}
	doc := bson.M{
		"id":              seq,
		"uuid":            uuid.New().String(),
		"conversation_id": conversationID,
		"role":            role,
		"type":            msgType,
		"content":         content,
		"date_created":    time.Now(),
	}
	_, err = r.db.Collection("memori_conversation_message").InsertOne(ctx, doc)
	return err
}

func (r *mongoMessageRepo) Recent(ctx context.Context, conversationID int64, limit int) ([]StoredMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	cur, err := r.db.Collection("memori_conversation_message").Find(
		ctx,
		bson.M{"conversation_id": conversationID},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	return decodeMongoMessages(ctx, cur, limit)
}

func (r *mongoMessageRepo) LatestUserMessageID(ctx context.Context, conversationID int64) (int64, bool, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	var doc struct {
		ID int64 `bson:"id"`
	}
	err := r.db.Collection("memori_conversation_message").FindOne(
		ctx,
		bson.M{"conversation_id": conversationID, "role": "user"},
		options.FindOne().SetSort(bson.D{{Key: "id", Value: -1}}),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return doc.ID, true, nil
}

func (r *mongoMessageRepo) RecentForEntity(ctx context.Context, entityID int64, limit int) ([]StoredMessage, error) {
	if limit <= 0 {
		limit = 200
	}
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	sessionIDs, err := mongoIDs(ctx, r.db.Collection("memori_session"), bson.M{"entity_id": entityID})
	if err != nil {
		return nil, err
	}
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	conversationIDs, err := mongoIDs(ctx, r.db.Collection("memori_conversation"), bson.M{"session_id": bson.M{"$in": sessionIDs}})
	if err != nil {
		return nil, err
	}
	if len(conversationIDs) == 0 {
		return nil, nil
	}
	cur, err := r.db.Collection("memori_conversation_message").Find(
		ctx,
		bson.M{"conversation_id": bson.M{"$in": conversationIDs}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	return decodeMongoMessages(ctx, cur, limit)
}

func mongoIDs(ctx context.Context, coll *mongo.Collection, filter bson.M) ([]int64, error) {
	cur, err := coll.Find(ctx, filter, options.Find().SetProjection(bson.M{"id": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var ids []int64
	for cur.Next(ctx) {
		var doc struct {
			ID int64 `bson:"id"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		if doc.ID != 0 {
			ids = append(ids, doc.ID)
		}
	}
	return ids, cur.Err()
}

func decodeMongoMessages(ctx context.Context, cur *mongo.Cursor, capacity int) ([]StoredMessage, error) {
	defer cur.Close(ctx)
	out := make([]StoredMessage, 0, capacity)
	for cur.Next(ctx) {
		var doc struct {
			ID             int64     `bson:"id"`
			ConversationID int64     `bson:"conversation_id"`
			Role           string    `bson:"role"`
			Content        string    `bson:"content"`
			DateCreated    time.Time `bson:"date_created"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, StoredMessage{
			ID: doc.ID, ConversationID: doc.ConversationID,
			Role: doc.Role, Content: doc.Content, DateCreated: doc.DateCreated,
		})
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

type mongoEntityFactRepo struct {
	db *mongo.Database
}

func (r *mongoEntityFactRepo) Upsert(ctx context.Context, entityID int64, in FactInput) (int64, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	now := time.Now()
	assertedBy, confidence := normalizeAssertion(in.AssertedBy, in.Confidence)
	set := bson.M{
		"content":           in.Content,
		"content_embedding": in.Embedding,
		"date_last_time":    now,
		"date_updated":      now,
		"slot":              in.Slot,
		"embedding_model":   in.EmbeddingModel,
		"embedding_dim":     in.EmbeddingDim,
		"search_tokens":     textutil.SearchTokens(in.Content),
		"confidence":        confidence,
		"asserted_by":       assertedBy,
	}
	if !in.PreserveInactive {
		set["status"] = "active"
		set["valid_to"] = nil
	}
	if in.SourceConversationID != nil {
		set["source_conversation_id"] = *in.SourceConversationID
	}
	if in.SourceMessageID != nil {
		set["source_message_id"] = *in.SourceMessageID
	}

	filter := bson.M{"entity_id": entityID, "uniq": in.Uniq}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		seq, err := nextSeq(ctx, r.db, "memori_entity_fact")
		if err != nil {
			return 0, err
		}
		setOnInsert := bson.M{
			"id":           seq,
			"uuid":         uuid.New().String(),
			"date_created": now,
			"valid_from":   now,
		}
		if in.PreserveInactive {
			// Do not overwrite an existing superseded/deleted status; a new
			// row is still created active.
			setOnInsert["status"] = "active"
		}
		update := bson.M{
			"$set":         set,
			"$inc":         bson.M{"num_times": int64(1)},
			"$setOnInsert": setOnInsert,
		}
		var doc struct {
			ID int64 `bson:"id"`
		}
		err = r.db.Collection("memori_entity_fact").FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
		if err == nil {
			return doc.ID, nil
		}
		lastErr = err
		if !mongo.IsDuplicateKeyError(err) {
			return 0, err
		}
	}
	return 0, lastErr
}

func (r *mongoEntityFactRepo) Touch(ctx context.Context, factID int64) error {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	now := time.Now()
	_, err := r.db.Collection("memori_entity_fact").UpdateOne(
		ctx,
		bson.M{"id": factID, "status": bson.M{"$nin": []string{"superseded", "deleted"}}},
		bson.M{
			"$inc": bson.M{"num_times": int64(1)},
			"$set": bson.M{"date_last_time": now, "date_updated": now},
		},
	)
	return err
}

func (r *mongoEntityFactRepo) HasInactiveUniq(ctx context.Context, entityID int64, uniq string) (bool, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	n, err := r.db.Collection("memori_entity_fact").CountDocuments(ctx, bson.M{
		"entity_id": entityID,
		"uniq":      uniq,
		"status":    bson.M{"$in": []string{"superseded", "deleted", "expired"}},
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *mongoEntityFactRepo) SupersedeSlot(ctx context.Context, entityID int64, slot string, exceptID int64) error {
	if slot == "" {
		return nil
	}
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	_, err := r.db.Collection("memori_entity_fact").UpdateMany(
		ctx,
		bson.M{
			"entity_id": entityID,
			"slot":      slot,
			"status":    bson.M{"$nin": []string{"superseded", "deleted"}},
			"id":        bson.M{"$ne": exceptID},
		},
		bson.M{"$set": bson.M{"status": "superseded", "valid_to": time.Now(), "date_updated": time.Now()}},
	)
	return err
}

func mongoConfidence(v float64) float64 {
	if v <= 0 {
		return 1
	}
	return v
}

func mongoAssertedBy(v string) string {
	if v == "" {
		return "user"
	}
	return v
}

func mongoActiveFilter() bson.M {
	// Missing status in legacy documents means active.
	return bson.M{"status": bson.M{"$nin": []string{"superseded", "deleted", "expired"}}}
}

func (r *mongoEntityFactRepo) findFacts(ctx context.Context, filter bson.M, limit int, sort bson.D) ([]FactResult, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	cur, err := r.db.Collection("memori_entity_fact").Find(
		ctx, filter,
		options.Find().SetSort(sort).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	out := make([]FactResult, 0, 64)
	for cur.Next(ctx) {
		var doc struct {
			ID                   int64      `bson:"id"`
			Content              string     `bson:"content"`
			Embedding            []byte     `bson:"content_embedding"`
			Slot                 string     `bson:"slot"`
			NumTimes             int64      `bson:"num_times"`
			DateLastTime         time.Time  `bson:"date_last_time"`
			EmbeddingModel       string     `bson:"embedding_model"`
			EmbeddingDim         int        `bson:"embedding_dim"`
			SourceConversationID *int64     `bson:"source_conversation_id"`
			ValidFrom            time.Time  `bson:"valid_from"`
			ValidTo              *time.Time `bson:"valid_to"`
			SourceMessageID      *int64     `bson:"source_message_id"`
			Confidence           float64    `bson:"confidence"`
			AssertedBy           string     `bson:"asserted_by"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, FactResult{
			ID:                   doc.ID,
			Content:              doc.Content,
			Embedding:            DecodeEmbedding(doc.Embedding),
			Slot:                 doc.Slot,
			NumTimes:             doc.NumTimes,
			DateLastTime:         doc.DateLastTime,
			EmbeddingModel:       doc.EmbeddingModel,
			EmbeddingDim:         doc.EmbeddingDim,
			SourceConversationID: doc.SourceConversationID,
			ValidFrom:            doc.ValidFrom,
			ValidTo:              doc.ValidTo,
			SourceMessageID:      doc.SourceMessageID,
			Confidence:           mongoConfidence(doc.Confidence),
			AssertedBy:           mongoAssertedBy(doc.AssertedBy),
		})
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *mongoEntityFactRepo) SearchLexical(ctx context.Context, entityID int64, query string, limit int) ([]FactResult, error) {
	tokens := textutil.SearchTokens(query)
	if len(tokens) == 0 {
		return nil, nil
	}
	if len(tokens) > 32 {
		tokens = tokens[:32]
	}
	if limit <= 0 {
		limit = 150
	}
	if limit > 500 {
		limit = 500
	}
	filter := bson.M{
		"entity_id":     entityID,
		"search_tokens": bson.M{"$in": tokens},
	}
	for k, v := range mongoActiveFilter() {
		filter[k] = v
	}
	return r.findFacts(ctx, filter, limit, bson.D{{Key: "date_last_time", Value: -1}, {Key: "id", Value: -1}})
}

func (r *mongoEntityFactRepo) Candidates(ctx context.Context, entityID int64, scanLimit int) ([]FactResult, error) {
	if scanLimit <= 0 {
		scanLimit = 2000
	}
	filter := bson.M{"entity_id": entityID}
	for k, v := range mongoActiveFilter() {
		filter[k] = v
	}
	return r.findFacts(ctx, filter, scanLimit, bson.D{
		{Key: "date_last_time", Value: -1},
		{Key: "_id", Value: -1},
	})
}

func (r *mongoEntityFactRepo) List(ctx context.Context, entityID int64, limit int) ([]FactResult, error) {
	if limit <= 0 {
		limit = 100
	}
	filter := bson.M{"entity_id": entityID}
	for k, v := range mongoActiveFilter() {
		filter[k] = v
	}
	return r.findFacts(ctx, filter, limit, bson.D{
		{Key: "date_last_time", Value: -1},
		{Key: "num_times", Value: -1},
	})
}

func (r *mongoEntityFactRepo) Forget(ctx context.Context, entityID int64, factID int64) error {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	_, err := r.db.Collection("memori_entity_fact").UpdateOne(
		ctx,
		bson.M{"id": factID, "entity_id": entityID},
		bson.M{"$set": bson.M{"status": "deleted", "valid_to": time.Now(), "date_updated": time.Now()}},
	)
	return err
}

func (r *mongoEntityFactRepo) Expirable(ctx context.Context, entityID int64, cutoff time.Time, protectTimes int64) ([]int64, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()
	filter := bson.M{
		"entity_id":      entityID,
		"status":         "active",
		"date_last_time": bson.M{"$lt": cutoff},
	}
	if protectTimes > 0 {
		filter["num_times"] = bson.M{"$lt": protectTimes}
	}
	cur, err := r.db.Collection("memori_entity_fact").Find(ctx, filter, options.Find().SetProjection(bson.M{"id": 1}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var ids []int64
	for cur.Next(ctx) {
		var doc struct {
			ID int64 `bson:"id"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		ids = append(ids, doc.ID)
	}
	return ids, cur.Err()
}

func (r *mongoEntityFactRepo) Expire(ctx context.Context, entityID int64, factIDs []int64, at time.Time) error {
	if len(factIDs) == 0 {
		return nil
	}
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()
	_, err := r.db.Collection("memori_entity_fact").UpdateMany(ctx, bson.M{
		"entity_id": entityID,
		"status":    "active",
		"id":        bson.M{"$in": factIDs},
	}, bson.M{"$set": bson.M{"status": "expired", "valid_to": at, "date_updated": at}})
	return err
}

func (r *mongoEntityFactRepo) Restore(ctx context.Context, entityID int64, factID int64) error {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()
	res, err := r.db.Collection("memori_entity_fact").UpdateOne(ctx, bson.M{
		"entity_id": entityID,
		"id":        factID,
		"status":    "expired",
	}, bson.M{"$set": bson.M{"status": "active", "valid_to": nil, "date_updated": time.Now()}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *mongoEntityFactRepo) Count(ctx context.Context, entityID int64) (int64, int64, error) {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	coll := r.db.Collection("memori_entity_fact")
	total, err := coll.CountDocuments(ctx, bson.M{"entity_id": entityID})
	if err != nil {
		return 0, 0, err
	}
	filter := bson.M{"entity_id": entityID}
	for k, v := range mongoActiveFilter() {
		filter[k] = v
	}
	active, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return 0, 0, err
	}
	return total, active, nil
}

func (r *mongoEntityFactRepo) ListForReembed(ctx context.Context, entityID int64, model string, dim int, limit int) ([]FactForReembed, error) {
	if limit <= 0 {
		limit = 32
	}
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	filter := bson.M{
		"entity_id": entityID,
		"$or": bson.A{
			bson.M{"embedding_model": bson.M{"$ne": model}},
			bson.M{"embedding_dim": bson.M{"$ne": dim}},
			bson.M{"embedding_model": bson.M{"$exists": false}},
			bson.M{"embedding_dim": bson.M{"$exists": false}},
		},
	}
	for k, v := range mongoActiveFilter() {
		filter[k] = v
	}
	cur, err := r.db.Collection("memori_entity_fact").Find(
		ctx, filter,
		options.Find().SetSort(bson.D{{Key: "id", Value: 1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var out []FactForReembed
	for cur.Next(ctx) {
		var doc struct {
			ID      int64  `bson:"id"`
			Content string `bson:"content"`
		}
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}
		out = append(out, FactForReembed{ID: doc.ID, Content: doc.Content})
	}
	return out, cur.Err()
}

func (r *mongoEntityFactRepo) UpdateEmbedding(ctx context.Context, factID int64, embedding []byte, model string, dim int) error {
	ctx, cancel := mongoOpCtx(ctx)
	defer cancel()

	_, err := r.db.Collection("memori_entity_fact").UpdateOne(
		ctx,
		bson.M{"id": factID},
		bson.M{"$set": bson.M{
			"content_embedding": embedding,
			"embedding_model":   model,
			"embedding_dim":     dim,
			"date_updated":      time.Now(),
		}},
	)
	return err
}

func nextSeq(ctx context.Context, db *mongo.Database, name string) (int64, error) {
	var doc struct {
		Seq int64 `bson:"seq"`
	}
	err := db.Collection("memori_counters").FindOneAndUpdate(
		ctx,
		bson.M{"_id": name},
		bson.M{"$inc": bson.M{"seq": 1}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return 0, err
	}
	return doc.Seq, nil
}

type mongoReposBundle struct {
	entity       *mongoEntityRepo
	process      *mongoProcessRepo
	session      *mongoSessionRepo
	conversation *mongoConversationRepo
	message      *mongoMessageRepo
	entityFact   *mongoEntityFactRepo
	subject      *mongoSubjectRepo
}

func newMongoRepos(db *mongo.Database) Repos {
	return &mongoReposBundle{
		entity:       &mongoEntityRepo{db: db},
		process:      &mongoProcessRepo{db: db},
		session:      &mongoSessionRepo{db: db},
		conversation: &mongoConversationRepo{db: db},
		message:      &mongoMessageRepo{db: db},
		entityFact:   &mongoEntityFactRepo{db: db},
		subject:      &mongoSubjectRepo{db: db},
	}
}

func (b *mongoReposBundle) Entity() EntityRepo             { return b.entity }
func (b *mongoReposBundle) Process() ProcessRepo           { return b.process }
func (b *mongoReposBundle) Session() SessionRepo           { return b.session }
func (b *mongoReposBundle) Conversation() ConversationRepo { return b.conversation }
func (b *mongoReposBundle) Message() MessageRepo           { return b.message }
func (b *mongoReposBundle) EntityFact() EntityFactRepo     { return b.entityFact }
func (b *mongoReposBundle) Subject() SubjectRepo           { return b.subject }
