package pool

import (
	"context"
	"testing"
)

func TestPoolReset(t *testing.T) {
	var created int
	var evicted []int
	p := New[int](
		func(context.Context) (int, error) {
			created++
			return created, nil
		},
		WithSize[int](4),
		WithEvict[int](func(v int) {
			evicted = append(evicted, v)
		}),
	)

	v1, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	v2, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	p.Put(v1)
	p.Put(v2)

	p.Reset()
	if len(evicted) != 2 {
		t.Fatalf("expected 2 evicted items after Reset, got %v", evicted)
	}

	v3, err := p.Get()
	if err != nil {
		t.Fatal(err)
	}
	if v3 != 3 {
		t.Fatalf("expected fresh factory item 3 after Reset, got %d", v3)
	}
}
