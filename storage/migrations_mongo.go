package storage

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type mongoMigrationOp struct {
	Collection string
	Index      mongo.IndexModel
}

var mongoMigrations = map[int][]mongoMigrationOp{
	1: {
		{"memori_schema_version", mongo.IndexModel{
			Keys:    bson.D{{Key: "num", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_entity", mongo.IndexModel{
			Keys:    bson.D{{Key: "external_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_entity", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_process", mongo.IndexModel{
			Keys:    bson.D{{Key: "external_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_process", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_session", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_session", mongo.IndexModel{
			Keys:    bson.D{{Key: "entity_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		}},
		{"memori_session", mongo.IndexModel{
			Keys:    bson.D{{Key: "process_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true),
		}},
		{"memori_conversation", mongo.IndexModel{
			Keys:    bson.D{{Key: "session_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_conversation", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_conversation_message", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_conversation_message", mongo.IndexModel{
			Keys:    bson.D{{Key: "conversation_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_entity_fact", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_entity_fact", mongo.IndexModel{
			Keys:    bson.D{{Key: "entity_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_entity_fact", mongo.IndexModel{
			Keys:    bson.D{{Key: "entity_id", Value: 1}, {Key: "uniq", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_entity_fact", mongo.IndexModel{
			Keys:    bson.D{{Key: "entity_id", Value: 1}, {Key: "num_times", Value: -1}, {Key: "date_last_time", Value: -1}},
			Options: options.Index().SetName("idx_memori_entity_fact_entity_id_freq"),
		}},
		{"memori_entity_fact", mongo.IndexModel{
			Keys:    bson.D{{Key: "entity_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetName("idx_memori_entity_fact_embedding_search"),
		}},
		{"memori_process_attribute", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_process_attribute", mongo.IndexModel{
			Keys:    bson.D{{Key: "process_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_process_attribute", mongo.IndexModel{
			Keys:    bson.D{{Key: "process_id", Value: 1}, {Key: "uniq", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_subject", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_subject", mongo.IndexModel{
			Keys:    bson.D{{Key: "uniq", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_predicate", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_predicate", mongo.IndexModel{
			Keys:    bson.D{{Key: "uniq", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_object", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_object", mongo.IndexModel{
			Keys:    bson.D{{Key: "uniq", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_knowledge_graph", mongo.IndexModel{
			Keys:    bson.D{{Key: "uuid", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_knowledge_graph", mongo.IndexModel{
			Keys:    bson.D{{Key: "entity_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_knowledge_graph", mongo.IndexModel{
			Keys:    bson.D{{Key: "entity_id", Value: 1}, {Key: "subject_id", Value: 1}, {Key: "predicate_id", Value: 1}, {Key: "object_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_knowledge_graph", mongo.IndexModel{
			Keys:    bson.D{{Key: "subject_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_knowledge_graph", mongo.IndexModel{
			Keys:    bson.D{{Key: "predicate_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
		{"memori_knowledge_graph", mongo.IndexModel{
			Keys:    bson.D{{Key: "object_id", Value: 1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		}},
	},
}

func (d *MongoDriver) migrateMongo(ctx context.Context) error {
	currentVersion := d.getSchemaVersion(ctx)
	maxVersion := 1 // Currently only version 1

	if currentVersion >= maxVersion {
		return nil
	}

	for v := currentVersion + 1; v <= maxVersion; v++ {
		ops, ok := mongoMigrations[v]
		if !ok {
			continue
		}

		for _, op := range ops {
			coll := d.db().Collection(op.Collection)
			_, err := coll.Indexes().CreateOne(ctx, op.Index)
			if err != nil {
				// Ignore duplicate index errors
				if !mongo.IsDuplicateKeyError(err) {
					return err
				}
			}
		}

		// Update schema version
		svColl := d.db().Collection("memori_schema_version")
		_, err := svColl.ReplaceOne(
			ctx,
			bson.M{"num": currentVersion},
			bson.M{"num": v},
			options.Replace().SetUpsert(true),
		)
		if err != nil {
			return err
		}
		currentVersion = v
	}

	return nil
}

func (d *MongoDriver) getSchemaVersion(ctx context.Context) int {
	svColl := d.db().Collection("memori_schema_version")
	var doc struct {
		Num int `bson:"num"`
	}
	err := svColl.FindOne(ctx, bson.M{}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return 0
	}
	if err != nil {
		return 0
	}
	return doc.Num
}

