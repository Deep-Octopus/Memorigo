package memori

import "time"

type Fact struct {
	Content       string
	Score         float64
	NumTimes      int64
	DateLastTime  time.Time
	Conversation  any
	SourceFactID  any
	SourceEntityID any
}


