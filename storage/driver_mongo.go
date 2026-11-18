package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

type MongoDriver struct {
	a *MongoAdapter

	once sync.Once
	base Repos
}

func newMongoDriver(adapter Adapter) (Driver, error) {
	a, ok := adapter.(*MongoAdapter)
	if !ok {
		return nil, fmt.Errorf("mongo driver expects *MongoAdapter, got %T", adapter)
	}
	return &MongoDriver{a: a}, nil
}

func (d *MongoDriver) Dialect() string { return "mongodb" }

func (d *MongoDriver) baseRepos() Repos {
	d.once.Do(func() {
		d.base = newMongoRepos(d.a.DB)
	})
	return d.base
}

func (d *MongoDriver) Entity() EntityRepo             { return d.baseRepos().Entity() }
func (d *MongoDriver) Process() ProcessRepo           { return d.baseRepos().Process() }
func (d *MongoDriver) Session() SessionRepo           { return d.baseRepos().Session() }
func (d *MongoDriver) Conversation() ConversationRepo { return d.baseRepos().Conversation() }
func (d *MongoDriver) Message() MessageRepo           { return d.baseRepos().Message() }
func (d *MongoDriver) EntityFact() EntityFactRepo     { return d.baseRepos().EntityFact() }

// Transaction runs fn against the Mongo repositories. Standalone MongoDB does
// not support multi-document transactions, so the callback is not atomic;
// keeping the interface uniform lets the writer share one code path.
func (d *MongoDriver) Transaction(ctx context.Context, fn func(Repos) error) error {
	return fn(d.baseRepos())
}

func (d *MongoDriver) Migrate() error {
	if d.a == nil || d.a.DB == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return d.migrateMongo(ctx)
}

func (d *MongoDriver) db() *mongo.Database { return d.a.DB }
