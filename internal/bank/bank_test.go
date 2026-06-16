package bank

import (
	"context"
	"errors"
	"testing"
)

func TestMockClientIsIdempotent(t *testing.T) {
	client := NewMockClient(1, 0, 0)
	req := TransferRequest{IdempotencyKey: "idem-1", WalletID: "wallet-1", Amount: 100}

	first, err := client.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	second, err := client.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second transfer: %v", err)
	}

	if first != second {
		t.Fatalf("expected idempotent result, got %#v and %#v", first, second)
	}
}

func TestMockClientRejectedResultIsObservableByStatus(t *testing.T) {
	client := NewMockClient(1, 1, 0)
	req := TransferRequest{IdempotencyKey: "idem-1", WalletID: "wallet-1", Amount: 100}

	result, err := client.Transfer(context.Background(), req)
	if !errors.Is(err, ErrTransferRejected) {
		t.Fatalf("expected ErrTransferRejected, got %v", err)
	}
	if result.Status != TransferRejected {
		t.Fatalf("expected rejected status, got %s", result.Status)
	}

	status, err := client.Status(context.Background(), req.IdempotencyKey)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Status != TransferRejected {
		t.Fatalf("expected rejected status lookup, got %s", status.Status)
	}
}
