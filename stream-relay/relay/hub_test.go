package relay

import (
	"encoding/json"
	"sync"
	"testing"
)

func serialize(c Chunk) []byte {
	data, _ := json.Marshal(c)
	return data
}

func TestSubscribeAddsToSessionMap(t *testing.T) {
	h := NewHub(100)
	sub, err := h.Subscribe("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if h.SubscriberCount("session-1") != 1 {
		t.Fatal("expected 1 subscriber")
	}
	h.Unsubscribe("session-1", sub)
}

func TestUnsubscribeRemovesAndCleansUp(t *testing.T) {
	h := NewHub(100)
	sub, _ := h.Subscribe("session-1")
	h.Unsubscribe("session-1", sub)
	if h.SubscriberCount("session-1") != 0 {
		t.Fatal("expected 0 subscribers after unsubscribe")
	}
	select {
	case <-sub.Done:
	default:
		t.Fatal("expected Done channel to be closed")
	}
	if !sub.IsClosed() {
		t.Fatal("expected subscriber to be closed")
	}
}

func TestDoubleUnsubscribeDoesNotPanic(t *testing.T) {
	h := NewHub(100)
	sub, _ := h.Subscribe("session-1")
	h.Unsubscribe("session-1", sub)
	h.Unsubscribe("session-1", sub)
}

func TestPublishDeliversToAllSubscribers(t *testing.T) {
	h := NewHub(100)
	sub1, _ := h.Subscribe("session-1")
	sub2, _ := h.Subscribe("session-1")

	data := serialize(Chunk{SessionID: "session-1", Sequence: 1, Content: "hello", Type: "data"})
	h.Publish("session-1", data)

	got1 := <-sub1.Send
	got2 := <-sub2.Send

	var c1, c2 Chunk
	json.Unmarshal(got1, &c1)
	json.Unmarshal(got2, &c2)

	if c1.Content != "hello" || c2.Content != "hello" {
		t.Fatal("expected both subscribers to receive chunk")
	}

	h.Unsubscribe("session-1", sub1)
	h.Unsubscribe("session-1", sub2)
}

func TestPublishDoesNotDeliverToOtherSessions(t *testing.T) {
	h := NewHub(100)
	sub1, _ := h.Subscribe("session-1")
	sub2, _ := h.Subscribe("session-2")

	data := serialize(Chunk{SessionID: "session-1", Sequence: 1, Content: "hello", Type: "data"})
	h.Publish("session-1", data)

	<-sub1.Send

	select {
	case <-sub2.Send:
		t.Fatal("session-2 subscriber should not receive session-1 chunk")
	default:
	}

	h.Unsubscribe("session-1", sub1)
	h.Unsubscribe("session-2", sub2)
}

func TestPublishDropsWhenChannelFullAndIncrementsDropCounter(t *testing.T) {
	h := NewHub(100)
	sub, _ := h.Subscribe("session-1")

	data := serialize(Chunk{SessionID: "session-1", Sequence: 1, Content: "fill", Type: "data"})
	for i := 0; i < 256; i++ {
		h.Publish("session-1", data)
	}

	overflow := serialize(Chunk{SessionID: "session-1", Sequence: 999, Content: "overflow", Type: "data"})
	h.Publish("session-1", overflow)

	count := 0
	for {
		select {
		case <-sub.Send:
			count++
		default:
			goto done
		}
	}
done:
	if count != 256 {
		t.Fatalf("expected 256 chunks, got %d", count)
	}

	if sub.Drops() != 1 {
		t.Fatalf("expected 1 drop, got %d", sub.Drops())
	}

	h.Unsubscribe("session-1", sub)
}

func TestNextSequenceMonotonicallyIncreasing(t *testing.T) {
	h := NewHub(100)
	prev := 0
	for i := 0; i < 100; i++ {
		seq := h.NextSequence("session-1")
		if seq <= prev {
			t.Fatalf("expected sequence > %d, got %d", prev, seq)
		}
		prev = seq
	}
}

func TestNextSequenceIndependentAcrossSessions(t *testing.T) {
	h := NewHub(100)
	s1 := h.NextSequence("session-1")
	s2 := h.NextSequence("session-2")
	if s1 != 1 || s2 != 1 {
		t.Fatalf("expected both sessions to start at 1, got %d and %d", s1, s2)
	}
}

func TestCleanupSequence(t *testing.T) {
	h := NewHub(100)
	h.NextSequence("session-1")
	h.NextSequence("session-1")
	h.CleanupSequence("session-1")
	seq := h.NextSequence("session-1")
	if seq != 1 {
		t.Fatalf("expected sequence to reset to 1 after cleanup, got %d", seq)
	}
}

func TestConcurrentSubscribeUnsubscribePublish(t *testing.T) {
	h := NewHub(10000)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub, err := h.Subscribe("session-race")
			if err != nil {
				return
			}
			data := serialize(Chunk{SessionID: "session-race", Sequence: h.NextSequence("session-race"), Content: "data", Type: "data"})
			h.Publish("session-race", data)
			h.Unsubscribe("session-race", sub)
		}()
	}

	wg.Wait()
}

func TestSubscribeAfterUnsubscribeReuses(t *testing.T) {
	h := NewHub(100)
	sub1, _ := h.Subscribe("session-1")
	h.Unsubscribe("session-1", sub1)

	sub2, _ := h.Subscribe("session-1")
	if h.SubscriberCount("session-1") != 1 {
		t.Fatal("expected 1 subscriber after re-subscribe")
	}

	data := serialize(Chunk{SessionID: "session-1", Sequence: 1, Content: "reuse", Type: "data"})
	h.Publish("session-1", data)
	got := <-sub2.Send
	var c Chunk
	json.Unmarshal(got, &c)
	if c.Content != "reuse" {
		t.Fatal("expected re-subscribed subscriber to receive chunk")
	}
	h.Unsubscribe("session-1", sub2)
}

func TestMultipleSubscribersSameSessionAllReceive(t *testing.T) {
	h := NewHub(100)
	subs := make([]*Subscriber, 10)
	for i := range subs {
		subs[i], _ = h.Subscribe("session-multi")
	}

	data := serialize(Chunk{SessionID: "session-multi", Sequence: 1, Content: "broadcast", Type: "data"})
	h.Publish("session-multi", data)

	for i, sub := range subs {
		got := <-sub.Send
		var c Chunk
		json.Unmarshal(got, &c)
		if c.Content != "broadcast" {
			t.Fatalf("subscriber %d did not receive chunk", i)
		}
	}

	for _, sub := range subs {
		h.Unsubscribe("session-multi", sub)
	}
}

func TestConnectionLimit(t *testing.T) {
	h := NewHub(3)
	s1, _ := h.Subscribe("a")
	s2, _ := h.Subscribe("b")
	s3, _ := h.Subscribe("c")
	_, err := h.Subscribe("d")
	if err == nil {
		t.Fatal("expected error when exceeding max connections")
	}
	h.Unsubscribe("a", s1)
	s4, err := h.Subscribe("d")
	if err != nil {
		t.Fatalf("expected subscribe to succeed after unsubscribe: %v", err)
	}
	h.Unsubscribe("b", s2)
	h.Unsubscribe("c", s3)
	h.Unsubscribe("d", s4)
}

func TestActiveConnections(t *testing.T) {
	h := NewHub(100)
	if h.ActiveConnections() != 0 {
		t.Fatal("expected 0 active connections")
	}
	s1, _ := h.Subscribe("a")
	s2, _ := h.Subscribe("b")
	if h.ActiveConnections() != 2 {
		t.Fatalf("expected 2, got %d", h.ActiveConnections())
	}
	h.Unsubscribe("a", s1)
	if h.ActiveConnections() != 1 {
		t.Fatalf("expected 1, got %d", h.ActiveConnections())
	}
	h.Unsubscribe("b", s2)
}

func TestSubscriberDropCounter(t *testing.T) {
	sub := NewSubscriber(1)
	if sub.Drops() != 0 {
		t.Fatalf("expected 0 drops initially, got %d", sub.Drops())
	}
	sub.IncrementDrops()
	sub.IncrementDrops()
	sub.IncrementDrops()
	if sub.Drops() != 3 {
		t.Fatalf("expected 3 drops, got %d", sub.Drops())
	}
}

func TestCloseClosesDoneChannel(t *testing.T) {
	sub := NewSubscriber(10)
	sub.Close()
	select {
	case <-sub.Done:
	default:
		t.Fatal("expected Done channel to be closed")
	}
	if !sub.IsClosed() {
		t.Fatal("expected IsClosed to return true")
	}
}

func BenchmarkPublish(b *testing.B) {
	h := NewHub(10000)
	subs := make([]*Subscriber, 100)
	for i := range subs {
		subs[i], _ = h.Subscribe("bench-session")
	}

	data := serialize(Chunk{SessionID: "bench-session", Sequence: 1, Content: "data", Type: "data"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Publish("bench-session", data)
	}

	for _, sub := range subs {
		h.Unsubscribe("bench-session", sub)
	}
}

func BenchmarkNextSequence(b *testing.B) {
	h := NewHub(100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.NextSequence("bench-session")
	}
}
