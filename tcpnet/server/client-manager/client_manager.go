package client

// import (
// 	"context"
// 	"encoding/binary"
// 	"fmt"
// 	"net"
// 	"sync"
// 	"time"
// )

// type ClientManager struct {
// 	clients map[uint32]Client

// 	mu     sync.RWMutex
// 	ctx    context.Context
// 	cancel context.CancelFunc

// 	count int
// }

// func NewClientManager() *ClientManager {
// 	ctx, cancel := context.WithCancel(context.Background())
// 	cm := &ClientManager{
// 		clients: make(map[uint32]Client),
// 		ctx:     ctx,
// 		cancel:  cancel,
// 	}
// 	return cm
// }

// func (cm *ClientManager) Close() {
// 	cm.mu.Lock()
// 	defer cm.mu.Unlock()
// 	cm.cancel()
// 	for _, client := range cm.clients {
// 		client.Close()
// 	}
// 	cm.count = 0
// }

// func (cm *ClientManager) Register(client Client) {
// 	cm.mu.Lock()
// 	defer cm.mu.Unlock()
// 	if _, exists := cm.clients[client.GetClientId()]; !exists {
// 		cm.clients[client.GetClientId()] = client
// 		cm.count++
// 	}
// }

// func (cm *ClientManager) Unregister(clientId uint32) {
// 	cm.mu.Lock()
// 	defer cm.mu.Unlock()
// 	if _, exists := cm.clients[clientId]; exists {
// 		delete(cm.clients, clientId)
// 		cm.count--
// 	}
// }

// func (cm *ClientManager) GetClientCount() int {
// 	cm.mu.RLock()
// 	defer cm.mu.RUnlock()
// 	return cm.count
// }

// func (cm *ClientManager) GetClient(clientId uint32) Client {
// 	cm.mu.RLock()
// 	defer cm.mu.RUnlock()
// 	if client, exists := cm.clients[clientId]; exists {
// 		return client
// 	}
// 	return nil
// }

// // 클라이언트의 아이디를 갱신하고 인증 여부를 업데이트 한다.
// func (cm *ClientManager) RenewClientId(old, new uint32) {
// 	cm.mu.RLock()
// 	defer cm.mu.RUnlock()
// 	if client, exists := cm.clients[old]; exists {
// 		client.SetClientId(new)
// 		client.SetAuth(true)
// 		cm.clients[new] = client
// 		delete(cm.clients, old)
// 	}
// }

// func (cm *ClientManager) DisconnectClient(clientId uint32) {
// 	if client, exists := cm.clients[clientId]; exists {
// 		client.Close()
// 		cm.Unregister(clientId)
// 	}
// }

// func (cm *ClientManager) SendToClientNoReply(clientId uint32, opCode byte, data []byte, sendTimeout time.Duration) (response customEvent.TCPResponse) {
// 	cm.mu.RLock()
// 	client, exists := cm.clients[clientId]
// 	cm.mu.RUnlock()

// 	if !exists {
// 		//response.Data = 3
// 		logger.Printf("[TCP] client not found: %d, in processing op_code: %+v", clientId, opCode)
// 		response.Err = ErrNormal
// 		return
// 	}

// 	seqNum := (client.GetSequenceNum() + 1) % SequenceMode

// 	message, err := serializer.SerializeResponse(binary.BigEndian, 0x00, opCode, seqNum, data)
// 	if err != nil {
// 		//response.Data = 3
// 		logger.Println("[TCP] failed to serialize message")
// 		response.Err = ErrNormal
// 		return
// 	}

// 	if err := client.SendMessage(message, sendTimeout); err != nil {
// 		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
// 			response.Err = ErrTimeoutSend
// 		} else {
// 			response.Err = err
// 		}
// 		logger.Printf("[TCP] failed to sendmessage, err: %+v", err)
// 		return
// 	}
// 	client.SetSequenceNum(seqNum)
// 	return
// }

// // Data 값이 2: 타임아웃, 3: 서버내부 처리에러
// func (cm *ClientManager) SendToClientWithReply(clientId uint32, opCode byte, data []byte,
// 	sendTimeout time.Duration, replyTimeout time.Duration) (response customEvent.TCPResponse) {

// 	defer func() {
// 		if r := recover(); r != nil {
// 			response.Err = fmt.Errorf("[TCP] client wsarecv( panic ), client id: %d, panic: %v", clientId, r)
// 		}
// 	}()

// 	cm.mu.RLock()
// 	client, exists := cm.clients[clientId]
// 	cm.mu.RUnlock()

// 	if !exists {
// 		//response.Data = 3
// 		logger.Printf("[TCP] client not found: %d, in processing op_code: %+v", clientId, opCode)
// 		response.Err = ErrNormal
// 		return
// 	}

// 	cm.mu.Lock()
// 	if _, exists := client.ReplyCh[opCode]; exists {
// 		cm.mu.Unlock()
// 		//response.Data = 3
// 		logger.Printf("[TCP] already exists processing message: pending %+v reply, clinet id: %+v", opCode, clientId)
// 		response.Err = ErrNormal
// 		return
// 	}

// 	replyCh := make(chan *handler.ReplyMessage, 1)
// 	client.ReplyCh[opCode] = replyCh
// 	cm.mu.Unlock()

// 	defer func() {
// 		if replyCh != nil {
// 			close(replyCh)
// 			delete(client.ReplyCh, opCode)
// 		}
// 	}()

// 	seqNum := (client.GetSequenceNum() + 1) % SequenceMode
// 	message, _ := serializer.SerializeResponse(binary.BigEndian, 0x00, opCode, seqNum, data)

// 	if err := client.SendMessage(message, sendTimeout); err != nil {
// 		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
// 			response.Err = ErrTimeoutSend
// 		} else {
// 			response.Err = err
// 		}
// 		logger.Printf("[TCP] failed to sendmessage, err: %+v", err)
// 		return
// 	}

// 	client.SetSequenceNum(seqNum)

// 	if replyTimeout == 0 {
// 		replyTimeout = 5 * time.Second
// 	}

// 	select {
// 	case <-time.After(replyTimeout):
// 		logger.Printf("[TCP] %+v reply timeout, client id: %+v", opCode, clientId)
// 		response.Err = ErrTimeoutReply
// 		return
// 	case reply, ok := <-replyCh:
// 		if !ok {
// 			logger.Printf("[TCP] %+v reply channel closed, client id: %+v", opCode, clientId)
// 			response.Err = ErrNormal
// 			return
// 		}
// 		response.Result = reply.Result
// 		response.Err = reply.Err
// 		return
// 	}
// }
