package providers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetrierDo(t *testing.T) {
	transient := &StatusError{Status: 503}
	permanent := &StatusError{Status: 400}
	tests := []struct {
		name      string
		errs      []error // returned per call; nil = success
		retries   int
		wantCalls int
		wantErr   bool
	}{
		{"success first try", []error{nil}, 2, 1, false},
		{"transient then success", []error{transient, nil}, 2, 2, false},
		{"transient exhausts retries", []error{transient, transient, transient, transient}, 2, 3, true},
		{"permanent not retried", []error{permanent, nil}, 2, 1, true},
		{"429 retried", []error{&StatusError{Status: 429}, nil}, 2, 2, false},
		{"zero retries", []error{transient, nil}, 0, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			r := Retrier{Retries: tt.retries, Base: time.Millisecond}
			err := r.Do(context.Background(), func() error {
				e := tt.errs[calls]
				calls++
				return e
			})
			if calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRetrierHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Retrier{Retries: 5, Base: time.Hour}
	calls := 0
	err := r.Do(ctx, func() error { calls++; return &StatusError{Status: 500} })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{&StatusError{Status: 500}, true},
		{&StatusError{Status: 429}, true},
		{&StatusError{Status: 401}, false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
		{errors.New("boom"), false},
	}
	for _, tt := range tests {
		if got := IsTransient(tt.err); got != tt.want {
			t.Errorf("IsTransient(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
