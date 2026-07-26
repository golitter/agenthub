package stream

import (
	"sync"
	"testing"
)

func TestHubPublishSubscribe(t *testing.T) {
	ch, seq := Hub.Subscribe("test:1")
	if seq != 0 {
		t.Fatalf("expected initial seq 0, got %d", seq)
	}

	Hub.Publish("test:1", "data: hello")

	evt := <-ch
	if evt.Data != "data: hello" || evt.Seq != 1 {
		t.Fatalf("unexpected event: %+v", evt)
	}
}

func TestHubClose(t *testing.T) {
	ch, _ := Hub.Subscribe("test:2")

	Hub.Publish("test:2", "data: before-close")
	Hub.Close("test:2")

	// 应先收到已发布的事件，随后收到一个 Done 事件
	evt := <-ch
	if evt.Data != "data: before-close" {
		t.Fatalf("unexpected data: %s", evt.Data)
	}
	evt, ok := <-ch
	if !ok || !evt.Done {
		t.Fatalf("expected Done event, got %+v, ok=%v", evt, ok)
	}
}

func TestHubConcurrentPublish(t *testing.T) {
	const n = 100
	ch, _ := Hub.Subscribe("test:3")

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Hub.Publish("test:3", "data: concurrent")
		}()
	}
	wg.Wait()

	received := 0
	for received < n {
		<-ch
		received++
	}
}

func TestHubPublishNoSubscribers(t *testing.T) {
	// 不应 panic
	Hub.Publish("test:no-subs", "data: nobody-listening")
}
