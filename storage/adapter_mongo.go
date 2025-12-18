package storage

import (
	"go.mongodb.org/mongo-driver/mongo"
)

type MongoAdapter struct {
	DB *mongo.Database
}

func (a *MongoAdapter) Dialect() string { return "mongodb" }

func isMongoDB(conn any) bool {
	_, ok := conn.(*mongo.Database)
	return ok
}

func newMongoAdapter(conn any) (Adapter, error) {
	db := conn.(*mongo.Database)
	return &MongoAdapter{DB: db}, nil
}
