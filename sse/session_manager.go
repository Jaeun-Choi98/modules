package sse

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type SessionManager[T UserIdKey, K ClientKey] struct {
	sessions     map[string]*Session[T, K] // 세션 ID를 키로 하는 맵
	sessionIndex map[T]string              // 사용자 ID를 키로, 세션 ID를 값으로 하는 맵
	mu           sync.RWMutex
}

func NewSessionManager[T UserIdKey, K ClientKey]() *SessionManager[T, K] {
	return &SessionManager[T, K]{
		sessions:     make(map[string]*Session[T, K]),
		sessionIndex: make(map[T]string),
	}
}

// NewSession은 사용자 ID를 기반으로 새 세션을 생성
func (s *SessionManager[T, K]) NewSession(userId T) *Session[T, K] {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 이미 존재하는 세션이 있으면 반환
	if sessionId, exists := s.sessionIndex[userId]; exists {
		return s.sessions[sessionId]
	}
	// 새 세션 ID 생성
	sessionId := fmt.Sprintf("sess_%v_%d", userId, time.Now().UnixNano())
	session := NewUserSession[T, K](sessionId, userId)

	s.sessions[sessionId] = session
	s.sessionIndex[userId] = sessionId

	return session
}

// GetSessionByID는 세션 ID로 세션을 조회
func (s *SessionManager[T, K]) GetSessionById(sessionId string) *Session[T, K] {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[sessionId]
}

// GetSessionByUserID는 사용자 ID로 세션을 조회
func (s *SessionManager[T, K]) GetSessionByUserId(userId T) *Session[T, K] {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sessionID, exists := s.sessionIndex[userId]; exists {
		return s.sessions[sessionID]
	}
	return nil
}

// RemoveSession은 세션을 제거
func (s *SessionManager[T, K]) RemoveSession(userId T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sessionId, exists := s.sessionIndex[userId]; exists {

		// 연결 종료
		if session, exists := s.sessions[sessionId]; exists {
			session.Close()
		}

		log.Printf("Closed session[userId:%v, sessionId:%s]", userId, sessionId)
		// 인덱스에서 제거
		delete(s.sessionIndex, userId)

		// 세션 맵에서 제거
		delete(s.sessions, sessionId)
	}
}

func (s *SessionManager[T, K]) Broadcast(evt Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, session := range s.sessions {
		session.Broadcast(evt)
	}
}

// Close는 모든 세션을 정리
func (s *SessionManager[T, K]) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 모든 세션 종료
	for _, session := range s.sessions {
		session.Close()
	}

	// 맵 초기화
	s.sessions = make(map[string]*Session[T, K])
	s.sessionIndex = make(map[T]string)
}
