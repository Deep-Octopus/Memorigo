package storage

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func decodeAnyTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		return parseTimeString(x)
	case []byte:
		return parseTimeString(string(x))
	default:
		return time.Time{}, false
	}
}

func parseTimeString(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	// Common layouts:
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05", // SQLite datetime('now')
		"2006-01-02 15:04:05.999999999",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Repos interface for driver operations
type Repos interface {
	Entity() EntityRepo
	Process() ProcessRepo
	Session() SessionRepo
	Conversation() ConversationRepo
	Message() MessageRepo
	EntityFact() EntityFactRepo
}

type EntityRepo interface {
	Create(externalID string) (int64, error)
	GetByExternalID(externalID string) (int64, error)
}

type ProcessRepo interface {
	Create(externalID string) (int64, error)
	GetByExternalID(externalID string) (int64, error)
}

type SessionRepo interface {
	Create(entityID, processID *int64, sessionUUID uuid.UUID) (int64, error)
	GetByUUID(sessionUUID uuid.UUID) (int64, error)
}

type ConversationRepo interface {
	Create(sessionID int64, timeoutMinutes int) (int64, error)
	GetBySessionID(sessionID int64) (int64, error)
	UpdateSummary(conversationID int64, summary string) error
}

type MessageRepo interface {
	Create(conversationID int64, role, msgType, content string) error
}

type EntityFactRepo interface {
	Create(entityID int64, content string, embedding []byte, uniq string) error
	Upsert(entityID int64, content string, embedding []byte, uniq string) error
	SearchByEmbedding(entityID int64, queryEmbedding []float32, limit, embeddingsLimit int) ([]FactResult, error)
}

type FactResult struct {
	Content      string
	Score        float64
	NumTimes     int64
	DateLastTime time.Time
}

// SQL repos implementation
type sqlEntityRepo struct {
	db      *sql.DB
	dialect string
}

func (r *sqlEntityRepo) placeholder(n int) string {
	if r.dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func (r *sqlEntityRepo) Create(externalID string) (int64, error) {
	// Try get existing first
	if id, err := r.GetByExternalID(externalID); err == nil {
		return id, nil
	}

	var id int64
	u := uuid.New().String()
	now := time.Now()

	var query string
	if r.dialect == "postgres" {
		query = "INSERT INTO memori_entity (uuid, external_id, date_created) VALUES ($1, $2, $3) RETURNING id"
	} else {
		query = "INSERT INTO memori_entity (uuid, external_id, date_created) VALUES (?, ?, ?) RETURNING id"
	}

	err := r.db.QueryRow(query, u, externalID, now).Scan(&id)
	if err != nil {
		// Fallback to existing (handles unique constraint races)
		return r.GetByExternalID(externalID)
	}
	return id, nil
}

func (r *sqlEntityRepo) GetByExternalID(externalID string) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_entity WHERE external_id = " + r.placeholder(1)
	err := r.db.QueryRow(query, externalID).Scan(&id)
	return id, err
}

type sqlProcessRepo struct {
	db      *sql.DB
	dialect string
}

func (r *sqlProcessRepo) Create(externalID string) (int64, error) {
	u := uuid.New().String()
	var id int64
	now := time.Now()

	var query string
	if r.dialect == "postgres" {
		query = "INSERT INTO memori_process (uuid, external_id, date_created) VALUES ($1, $2, $3) RETURNING id"
	} else {
		query = "INSERT INTO memori_process (uuid, external_id, date_created) VALUES (?, ?, ?) RETURNING id"
	}

	err := r.db.QueryRow(
		query,
		u, externalID, now,
	).Scan(&id)
	if err != nil {
		return r.GetByExternalID(externalID)
	}
	return id, nil
}

func (r *sqlProcessRepo) GetByExternalID(externalID string) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_process WHERE external_id = " + func() string {
		if r.dialect == "postgres" {
			return "$1"
		}
		return "?"
	}()
	err := r.db.QueryRow(query, externalID).Scan(&id)
	return id, err
}

type sqlSessionRepo struct {
	db      *sql.DB
	dialect string
}

func (r *sqlSessionRepo) Create(entityID, processID *int64, sessionUUID uuid.UUID) (int64, error) {
	var id int64
	now := time.Now()

	var query string
	if r.dialect == "postgres" {
		query = "INSERT INTO memori_session (uuid, entity_id, process_id, date_created) VALUES ($1, $2, $3, $4) RETURNING id"
	} else {
		query = "INSERT INTO memori_session (uuid, entity_id, process_id, date_created) VALUES (?, ?, ?, ?) RETURNING id"
	}

	err := r.db.QueryRow(
		query,
		sessionUUID.String(), entityID, processID, now,
	).Scan(&id)
	if err != nil {
		return r.GetByUUID(sessionUUID)
	}
	return id, nil
}

func (r *sqlSessionRepo) GetByUUID(sessionUUID uuid.UUID) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_session WHERE uuid = " + func() string {
		if r.dialect == "postgres" {
			return "$1"
		}
		return "?"
	}()
	err := r.db.QueryRow(query, sessionUUID.String()).Scan(&id)
	return id, err
}

type sqlConversationRepo struct {
	db      *sql.DB
	dialect string
}

func (r *sqlConversationRepo) Create(sessionID int64, timeoutMinutes int) (int64, error) {
	// Check if existing conversation is still valid
	var existingID sql.NullInt64
	var existingCreated any
	querySel := "SELECT id, date_created FROM memori_conversation WHERE session_id = %s ORDER BY date_created DESC LIMIT 1"
	ph := "?"
	if r.dialect == "postgres" {
		ph = "$1"
	}
	err := r.db.QueryRow(
		fmt.Sprintf(querySel, ph),
		sessionID,
	).Scan(&existingID, &existingCreated)
	if err == nil && existingID.Valid {
		if createdAt, ok := decodeAnyTime(existingCreated); ok {
			age := time.Since(createdAt)
			if age < time.Duration(timeoutMinutes)*time.Minute {
				return existingID.Int64, nil
			}
		}
	}

	u := uuid.New().String()
	var id int64
	now := time.Now()
	var queryIns string
	if r.dialect == "postgres" {
		queryIns = "INSERT INTO memori_conversation (uuid, session_id, date_created) VALUES ($1, $2, $3) RETURNING id"
	} else {
		queryIns = "INSERT INTO memori_conversation (uuid, session_id, date_created) VALUES (?, ?, ?) RETURNING id"
	}
	err = r.db.QueryRow(
		queryIns,
		u, sessionID, now,
	).Scan(&id)
	return id, err
}

func (r *sqlConversationRepo) GetBySessionID(sessionID int64) (int64, error) {
	var id int64
	query := "SELECT id FROM memori_conversation WHERE session_id = %s ORDER BY date_created DESC LIMIT 1"
	ph := "?"
	if r.dialect == "postgres" {
		ph = "$1"
	}
	err := r.db.QueryRow(fmt.Sprintf(query, ph), sessionID).Scan(&id)
	return id, err
}

func (r *sqlConversationRepo) UpdateSummary(conversationID int64, summary string) error {
	now := time.Now()
	var query string
	if r.dialect == "postgres" {
		query = "UPDATE memori_conversation SET summary = $1, date_updated = $2 WHERE id = $3"
	} else {
		query = "UPDATE memori_conversation SET summary = ?, date_updated = ? WHERE id = ?"
	}
	_, err := r.db.Exec(query, summary, now, conversationID)
	return err
}

type sqlMessageRepo struct {
	db      *sql.DB
	dialect string
}

func (r *sqlMessageRepo) Create(conversationID int64, role, msgType, content string) error {
	u := uuid.New().String()
	now := time.Now()
	var query string
	if r.dialect == "postgres" {
		query = "INSERT INTO memori_conversation_message (uuid, conversation_id, role, type, content, date_created) VALUES ($1, $2, $3, $4, $5, $6)"
	} else {
		query = "INSERT INTO memori_conversation_message (uuid, conversation_id, role, type, content, date_created) VALUES (?, ?, ?, ?, ?, ?)"
	}
	_, err := r.db.Exec(
		query,
		u, conversationID, role, msgType, content, now,
	)
	return err
}

type sqlEntityFactRepo struct {
	db      *sql.DB
	dialect string
}

func (r *sqlEntityFactRepo) Create(entityID int64, content string, embedding []byte, uniq string) error {
	u := uuid.New().String()
	now := time.Now()
	var query string
	if r.dialect == "postgres" {
		query = "INSERT INTO memori_entity_fact (uuid, entity_id, content, content_embedding, num_times, date_last_time, uniq, date_created) VALUES ($1, $2, $3, $4, 1, $5, $6, $7)"
	} else {
		query = "INSERT INTO memori_entity_fact (uuid, entity_id, content, content_embedding, num_times, date_last_time, uniq, date_created) VALUES (?, ?, ?, ?, 1, ?, ?, ?)"
	}
	_, err := r.db.Exec(
		query,
		u, entityID, content, embedding, now, uniq, now,
	)
	return err
}

func (r *sqlEntityFactRepo) Upsert(entityID int64, content string, embedding []byte, uniq string) error {
	u := uuid.New().String()
	now := time.Now()
	var query string
	if r.dialect == "postgres" {
		query = `INSERT INTO memori_entity_fact (uuid, entity_id, content, content_embedding, num_times, date_last_time, uniq, date_created)
		 VALUES ($1, $2, $3, $4, 1, $5, $6, $7)
		 ON CONFLICT(entity_id, uniq) DO UPDATE SET
			num_times = memori_entity_fact.num_times + 1,
			date_last_time = $8,
			date_updated = $9`
	} else {
		query = `INSERT INTO memori_entity_fact (uuid, entity_id, content, content_embedding, num_times, date_last_time, uniq, date_created)
		 VALUES (?, ?, ?, ?, 1, ?, ?, ?)
		 ON CONFLICT(entity_id, uniq) DO UPDATE SET
			num_times = num_times + 1,
			date_last_time = ?,
			date_updated = ?`
	}
	_, err := r.db.Exec(
		query,
		u, entityID, content, embedding, now, uniq, now, now, now,
	)
	return err
}

func (r *sqlEntityFactRepo) SearchByEmbedding(entityID int64, queryEmbedding []float32, limit, embeddingsLimit int) ([]FactResult, error) {
	// Fetch facts and compute cosine similarity in memory.
	var query string
	if r.dialect == "postgres" {
		query = "SELECT content, content_embedding, num_times, date_last_time FROM memori_entity_fact WHERE entity_id = $1 LIMIT $2"
	} else {
		query = "SELECT content, content_embedding, num_times, date_last_time FROM memori_entity_fact WHERE entity_id = ? LIMIT ?"
	}
	rows, err := r.db.Query(
		query,
		entityID, embeddingsLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FactResult
	for rows.Next() {
		var content string
		var embedding []byte
		var numTimes int64
		var dateLastAny any
		if err := rows.Scan(&content, &embedding, &numTimes, &dateLastAny); err != nil {
			continue
		}

		emb := decodeEmbedding(embedding)
		score := cosineSimilarity(queryEmbedding, emb)

		dateLastTime, _ := decodeAnyTime(dateLastAny)
		results = append(results, FactResult{
			Content:      content,
			Score:        score,
			NumTimes:     numTimes,
			DateLastTime: dateLastTime,
		})
	}

	// Sort by score (desc) and limit
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			// tie-breaker: more recent first
			return results[i].DateLastTime.After(results[j].DateLastTime)
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// decodeEmbedding converts little-endian []byte back to []float32.
func decodeEmbedding(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := 0; i < len(out); i++ {
		u := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(u)
	}
	return out
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// SQL driver repos
type sqlRepos struct {
	entity       EntityRepo
	process      ProcessRepo
	session      SessionRepo
	conversation ConversationRepo
	message      MessageRepo
	entityFact   EntityFactRepo
}

func (d *SQLDriver) Entity() EntityRepo {
	if d.repos == nil {
		d.repos = &sqlRepos{
			entity:       &sqlEntityRepo{db: d.db(), dialect: d.dialect},
			process:      &sqlProcessRepo{db: d.db(), dialect: d.dialect},
			session:      &sqlSessionRepo{db: d.db(), dialect: d.dialect},
			conversation: &sqlConversationRepo{db: d.db(), dialect: d.dialect},
			message:      &sqlMessageRepo{db: d.db(), dialect: d.dialect},
			entityFact:   &sqlEntityFactRepo{db: d.db(), dialect: d.dialect},
		}
	}
	return d.repos.entity
}

func (d *SQLDriver) Process() ProcessRepo {
	if d.repos == nil {
		d.Entity() // Initialize repos
	}
	return d.repos.process
}

func (d *SQLDriver) Session() SessionRepo {
	if d.repos == nil {
		d.Entity()
	}
	return d.repos.session
}

func (d *SQLDriver) Conversation() ConversationRepo {
	if d.repos == nil {
		d.Entity()
	}
	return d.repos.conversation
}

func (d *SQLDriver) EntityFact() EntityFactRepo {
	if d.repos == nil {
		d.Entity()
	}
	return d.repos.entityFact
}

func (d *SQLDriver) Message() MessageRepo {
	if d.repos == nil {
		d.Entity()
	}
	return d.repos.message
}

// MongoDB repos

type mongoEntityRepo struct {
	db *mongo.Database
}

func (r *mongoEntityRepo) Create(externalID string) (int64, error) {
	// Try existing first
	id, err := r.GetByExternalID(externalID)
	if err == nil {
		return id, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	seq, err := nextSeq(r.db, "memori_entity")
	if err != nil {
		return 0, err
	}

	doc := bson.M{
		"id":           seq,
		"uuid":         uuid.New().String(),
		"external_id":  externalID,
		"date_created": time.Now(),
	}

	coll := r.db.Collection("memori_entity")
	_, err = coll.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return r.GetByExternalID(externalID)
		}
		return 0, err
	}
	return seq, nil
}

func (r *mongoEntityRepo) GetByExternalID(externalID string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_entity")
	var doc struct {
		ID int64 `bson:"id"`
	}
	err := coll.FindOne(ctx, bson.M{"external_id": externalID}).Decode(&doc)
	if err != nil {
		return 0, err
	}
	return doc.ID, nil
}

type mongoProcessRepo struct {
	db *mongo.Database
}

func (r *mongoProcessRepo) Create(externalID string) (int64, error) {
	id, err := r.GetByExternalID(externalID)
	if err == nil {
		return id, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	seq, err := nextSeq(r.db, "memori_process")
	if err != nil {
		return 0, err
	}

	doc := bson.M{
		"id":           seq,
		"uuid":         uuid.New().String(),
		"external_id":  externalID,
		"date_created": time.Now(),
	}

	coll := r.db.Collection("memori_process")
	_, err = coll.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return r.GetByExternalID(externalID)
		}
		return 0, err
	}
	return seq, nil
}

func (r *mongoProcessRepo) GetByExternalID(externalID string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_process")
	var doc struct {
		ID int64 `bson:"id"`
	}
	err := coll.FindOne(ctx, bson.M{"external_id": externalID}).Decode(&doc)
	if err != nil {
		return 0, err
	}
	return doc.ID, nil
}

type mongoSessionRepo struct {
	db *mongo.Database
}

func (r *mongoSessionRepo) Create(entityID, processID *int64, sessionUUID uuid.UUID) (int64, error) {
	// If session with this UUID exists, return it
	if id, err := r.GetByUUID(sessionUUID); err == nil {
		return id, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	seq, err := nextSeq(r.db, "memori_session")
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

	coll := r.db.Collection("memori_session")
	_, err = coll.InsertOne(ctx, doc)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

func (r *mongoSessionRepo) GetByUUID(sessionUUID uuid.UUID) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_session")
	var doc struct {
		ID int64 `bson:"id"`
	}
	err := coll.FindOne(ctx, bson.M{"uuid": sessionUUID.String()}).Decode(&doc)
	if err != nil {
		return 0, err
	}
	return doc.ID, nil
}

type mongoConversationRepo struct {
	db *mongo.Database
}

func (r *mongoConversationRepo) Create(sessionID int64, timeoutMinutes int) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_conversation")

	// Try to reuse recent conversation for this session
	var existing struct {
		ID          int64     `bson:"id"`
		DateCreated time.Time `bson:"date_created"`
	}
	err := coll.FindOne(
		ctx,
		bson.M{"session_id": sessionID},
		options.FindOne().SetSort(bson.D{{Key: "date_created", Value: -1}}),
	).Decode(&existing)
	if err == nil {
		if time.Since(existing.DateCreated) < time.Duration(timeoutMinutes)*time.Minute {
			return existing.ID, nil
		}
	}

	seq, err := nextSeq(r.db, "memori_conversation")
	if err != nil {
		return 0, err
	}

	doc := bson.M{
		"id":           seq,
		"uuid":         uuid.New().String(),
		"session_id":   sessionID,
		"date_created": time.Now(),
	}
	_, err = coll.InsertOne(ctx, doc)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

func (r *mongoConversationRepo) GetBySessionID(sessionID int64) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_conversation")
	var doc struct {
		ID int64 `bson:"id"`
	}
	err := coll.FindOne(
		ctx,
		bson.M{"session_id": sessionID},
		options.FindOne().SetSort(bson.D{{Key: "date_created", Value: -1}}),
	).Decode(&doc)
	if err != nil {
		return 0, err
	}
	return doc.ID, nil
}

func (r *mongoConversationRepo) UpdateSummary(conversationID int64, summary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_conversation")
	_, err := coll.UpdateOne(
		ctx,
		bson.M{"id": conversationID},
		bson.M{
			"$set": bson.M{
				"summary":      summary,
				"date_updated": time.Now(),
			},
		},
	)
	return err
}

type mongoMessageRepo struct {
	db *mongo.Database
}

func (r *mongoMessageRepo) Create(conversationID int64, role, msgType, content string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_conversation_message")
	doc := bson.M{
		"uuid":            uuid.New().String(),
		"conversation_id": conversationID,
		"role":            role,
		"type":            msgType,
		"content":         content,
		"date_created":    time.Now(),
	}
	_, err := coll.InsertOne(ctx, doc)
	return err
}

type mongoEntityFactRepo struct {
	db *mongo.Database
}

func (r *mongoEntityFactRepo) Create(entityID int64, content string, embedding []byte, uniq string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_entity_fact")
	doc := bson.M{
		"uuid":              uuid.New().String(),
		"entity_id":         entityID,
		"content":           content,
		"content_embedding": embedding,
		"num_times":         int64(1),
		"date_last_time":    time.Now(),
		"uniq":              uniq,
		"date_created":      time.Now(),
	}
	_, err := coll.InsertOne(ctx, doc)
	return err
}

func (r *mongoEntityFactRepo) Upsert(entityID int64, content string, embedding []byte, uniq string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_entity_fact")
	filter := bson.M{"entity_id": entityID, "uniq": uniq}
	now := time.Now()
	update := bson.M{
		"$setOnInsert": bson.M{
			"uuid":         uuid.New().String(),
			"date_created": now,
		},
		"$set": bson.M{
			"content":           content,
			"content_embedding": embedding,
			"date_last_time":    now,
			"date_updated":      now,
		},
		"$inc": bson.M{
			"num_times": int64(1),
		},
	}
	_, err := coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

func (r *mongoEntityFactRepo) SearchByEmbedding(entityID int64, queryEmbedding []float32, limit, embeddingsLimit int) ([]FactResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.db.Collection("memori_entity_fact")

	cur, err := coll.Find(
		ctx,
		bson.M{"entity_id": entityID},
		options.Find().SetLimit(int64(embeddingsLimit)),
	)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var results []FactResult
	for cur.Next(ctx) {
		var doc struct {
			Content      string    `bson:"content"`
			Embedding    []byte    `bson:"content_embedding"`
			NumTimes     int64     `bson:"num_times"`
			DateLastTime time.Time `bson:"date_last_time"`
		}
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		emb := decodeEmbedding(doc.Embedding)
		score := cosineSimilarity(queryEmbedding, emb)
		results = append(results, FactResult{
			Content:      doc.Content,
			Score:        score,
			NumTimes:     doc.NumTimes,
			DateLastTime: doc.DateLastTime,
		})
	}

	// Sort by score and recency
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].DateLastTime.After(results[j].DateLastTime)
		}
		return results[i].Score > results[j].Score
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// wire Mongo repos into MongoDriver

func (d *MongoDriver) Entity() EntityRepo {
	return &mongoEntityRepo{db: d.db()}
}

func (d *MongoDriver) Process() ProcessRepo {
	return &mongoProcessRepo{db: d.db()}
}

func (d *MongoDriver) Session() SessionRepo {
	return &mongoSessionRepo{db: d.db()}
}

func (d *MongoDriver) Conversation() ConversationRepo {
	return &mongoConversationRepo{db: d.db()}
}

func (d *MongoDriver) EntityFact() EntityFactRepo {
	return &mongoEntityFactRepo{db: d.db()}
}

func (d *MongoDriver) Message() MessageRepo {
	return &mongoMessageRepo{db: d.db()}
}

// sequence helper for Mongo collections

func nextSeq(db *mongo.Database, name string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := db.Collection("memori_counters")
	var doc struct {
		Seq int64 `bson:"seq"`
	}
	err := coll.FindOneAndUpdate(
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
