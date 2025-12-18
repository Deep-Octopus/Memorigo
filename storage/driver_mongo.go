package storage

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

type MongoDriver struct {
	a *MongoAdapter
}

func newMongoDriver(adapter Adapter) (Driver, error) {
	a, ok := adapter.(*MongoAdapter)
	if !ok {
		return nil, fmt.Errorf("mongo driver expects *MongoAdapter, got %T", adapter)
	}
	return &MongoDriver{a: a}, nil
}

func (d *MongoDriver) Dialect() string { return "mongodb" }

func (d *MongoDriver) Migrate() error {
	if d.a == nil || d.a.DB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return d.migrateMongo(ctx)
}

func (d *MongoDriver) db() *mongo.Database { return d.a.DB }


