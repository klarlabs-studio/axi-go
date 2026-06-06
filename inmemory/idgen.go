package inmemory

import (
	"fmt"
	"sync/atomic"

	"go.klarlabs.de/axi/domain"
)

// SequentialIDGenerator generates sequential session IDs (for testing).
type SequentialIDGenerator struct {
	counter atomic.Int64
}

func NewSequentialIDGenerator() *SequentialIDGenerator {
	return &SequentialIDGenerator{}
}

func (g *SequentialIDGenerator) GenerateSessionID() domain.ExecutionSessionID {
	n := g.counter.Add(1)
	return domain.ExecutionSessionID(fmt.Sprintf("session-%d", n))
}
