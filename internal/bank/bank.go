package bank

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

var ErrTransferRejected = errors.New("bank rejected transfer")

type TransferRequest struct {
	IdempotencyKey string
	WalletID       string
	Amount         int64
}

type TransferResult struct {
	Reference string
	Status    TransferStatus
}

type TransferStatus string

const (
	TransferSucceeded TransferStatus = "SUCCEEDED"
	TransferRejected  TransferStatus = "REJECTED"
	TransferUnknown   TransferStatus = "UNKNOWN"
)

type Client interface {
	Transfer(ctx context.Context, req TransferRequest) (TransferResult, error)
	Status(ctx context.Context, idempotencyKey string) (TransferResult, error)
}

type MockClient struct {
	mu               sync.Mutex
	rand             *rand.Rand
	failureRate      float64
	networkErrorRate float64
	results          map[string]TransferResult
}

func NewMockClient(seed int64, failureRate, networkErrorRate float64) *MockClient {
	return &MockClient{
		rand:             rand.New(rand.NewSource(seed)),
		failureRate:      failureRate,
		networkErrorRate: networkErrorRate,
		results:          make(map[string]TransferResult),
	}
}

func (c *MockClient) Transfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return TransferResult{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.results[req.IdempotencyKey]; ok {
		if existing.Status == TransferRejected {
			return existing, ErrTransferRejected
		}
		return existing, nil
	}

	if c.rand.Float64() < c.networkErrorRate {
		return TransferResult{Status: TransferUnknown}, context.DeadlineExceeded
	}

	result := TransferResult{
		Reference: fmt.Sprintf("mock-bank-%s", req.IdempotencyKey),
		Status:    TransferSucceeded,
	}
	if c.rand.Float64() < c.failureRate {
		result.Status = TransferRejected
		c.results[req.IdempotencyKey] = result
		return result, ErrTransferRejected
	}

	c.results[req.IdempotencyKey] = result
	return result, nil
}

func (c *MockClient) Status(ctx context.Context, idempotencyKey string) (TransferResult, error) {
	if err := ctx.Err(); err != nil {
		return TransferResult{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	result, ok := c.results[idempotencyKey]
	if !ok {
		return TransferResult{Status: TransferUnknown}, nil
	}
	return result, nil
}

func NewDefaultMockClient(failureRate, networkErrorRate float64) *MockClient {
	return NewMockClient(time.Now().UnixNano(), failureRate, networkErrorRate)
}
