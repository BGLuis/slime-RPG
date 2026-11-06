package event_test

import (
	"sync"
	"testing"
	"time"

	"github.com/BGLuis/slime-rpg/pkg/event"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch1 := bus.Subscribe(10)
	ch2 := bus.Subscribe(10)

	ev := event.ProgressEvent{
		File:    "Map001.json",
		Status:  event.StatusProcessing,
		Message: "Translating...",
		Current: 5,
		Total:   10,
	}

	bus.Publish(ev)

	select {
	case received := <-ch1:
		if received.File != ev.File || received.Status != ev.Status {
			t.Errorf("ch1 recebeu evento incorreto: %+v", received)
		}
		if received.Timestamp.IsZero() {
			t.Errorf("ch1 esperava timestamp preenchido automaticamente")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout aguardando evento no ch1")
	}

	select {
	case received := <-ch2:
		if received.File != ev.File {
			t.Errorf("ch2 recebeu evento incorreto: %+v", received)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout aguardando evento no ch2")
	}
}

func TestEventBus_Unsubscribe(t *testing.T) {
	bus := event.NewBus()
	ch := bus.Subscribe(5)

	bus.Unsubscribe(ch)

	// O canal deve ser fechado após o unsubscribe
	_, open := <-ch
	if open {
		t.Errorf("esperava canal fechado após unsubscribe")
	}

	// Publicar não deve causar panic nem travar
	bus.Publish(event.ProgressEvent{File: "teste.json"})
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := event.NewBus()
	defer bus.Close()

	ch := bus.Subscribe(500)

	const workers = 10
	const eventsPerWorker = 20

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < eventsPerWorker; j++ {
				bus.Publish(event.ProgressEvent{
					File:   "file.json",
					Status: event.StatusProcessing,
				})
			}
		}(i)
	}

	wg.Wait()

	receivedCount := 0
drain:
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				break drain
			}
			receivedCount++
			if receivedCount == workers*eventsPerWorker {
				break drain
			}
		case <-time.After(50 * time.Millisecond):
			break drain
		}
	}

	if receivedCount != workers*eventsPerWorker {
		t.Errorf("esperava receber %d eventos, recebeu %d", workers*eventsPerWorker, receivedCount)
	}
}

func TestEventBus_Close(t *testing.T) {
	bus := event.NewBus()
	ch := bus.Subscribe(5)

	bus.Close()

	_, open := <-ch
	if open {
		t.Errorf("esperava que o canal estivesse fechado após bus.Close()")
	}

	// Novas inscrições após o fechamento retornam canais já fechados
	chAfter := bus.Subscribe(5)
	_, openAfter := <-chAfter
	if openAfter {
		t.Errorf("esperava canal fechado para inscrição pós-Close")
	}
}
