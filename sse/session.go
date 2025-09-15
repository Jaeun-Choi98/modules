package sse

import (
	"log"
	"sync"
)

type UserIdKey interface {
	byte | int8 | int16 | uint16 | int | int32 | uint32 | int64 | uint64 | float32 | float64 | string
}

type Session[T UserIdKey, K ClientKey] struct {
	sessionId string
	userId    T
	clients   map[K]*SSEClient[T, K]
	mu        *sync.RWMutex
}

// NewUserSession은 새로운 사용자 세션을 생성
func NewUserSession[T UserIdKey, K ClientKey](sessionId string, userId T) *Session[T, K] {
	return &Session[T, K]{
		sessionId: sessionId,
		userId:    userId,
		clients:   make(map[K]*SSEClient[T, K]),
		mu:        &sync.RWMutex{},
	}
}

// AddClient는 새 SSE 클라이언트를 세션에 추가
func (s *Session[T, K]) AddClient(clientId K, client *SSEClient[T, K]) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[clientId] = client
}

// RemoveClient는 세션에서 SSE 클라이언트를 제거
func (s *Session[T, K]) RemoveClient(clientId K) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if client, exists := s.clients[clientId]; exists {
		client.Close()
		log.Printf("[SSE] closed client[userId: %v, clientId: %v]", s.userId, clientId)
		delete(s.clients, clientId)
	}
}

// Broadcast는 세션의 모든 클라이언트에게 이벤트를 전송
func (s *Session[T, K]) Broadcast(event Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for clientId, client := range s.clients {
		err := client.SendMessage(event)
		if err != nil {
			// 오류 발생 시 클라이언트 제거
			go s.RemoveClient(clientId)
		}
	}
}

func (s *Session[T, K]) Count() int {
	return len(s.clients)
}

// Close는 세션의 모든 클라이언트 연결을 종료
func (s *Session[T, K]) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, client := range s.clients {
		client.Close()
	}
	s.clients = make(map[K]*SSEClient[T, K])
}
