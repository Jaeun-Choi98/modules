package udpnet_test

import (
	"context"
	"log"
	"net"
	"time"

	"github.com/Jaeun-Choi98/modules/udpnet"
)

// 장비에 요청을 보내고, 핑퐁으로 장비가 수신 가능한 상태인지 감시하는 예.
// 장비는 6000번 포트에서 받고, 우리 쪽 고정 포트 7000번으로 응답함.
func ExampleEndpoint_client() {
	device := &net.UDPAddr{IP: net.ParseIP("192.168.0.10"), Port: 6000}

	ep, err := udpnet.NewEndpoint(context.Background(), udpnet.Config{
		LocalAddr: ":7000",
	})
	if err != nil {
		log.Fatal(err)
	}
	ep.SetHandler(func(data []byte, src *net.UDPAddr) {
		log.Printf("from %s: % x", src, data)
	})
	if err := ep.Start(); err != nil {
		log.Fatal(err)
	}
	defer ep.Shutdown()

	probe, _ := ep.StartProbe(udpnet.Probe{
		Target:   device.String(),
		Interval: time.Second,
		Ping:     []byte{0x01},
		IsPong:   func(data []byte, _ *net.UDPAddr) bool { return len(data) == 1 && data[0] == 0x81 },
		OnChange: func(reachable bool) { log.Printf("device reachable: %v", reachable) },
	})

	if probe.Reachable() {
		ep.SendTo([]byte{0x10, 0x00}, device)
	}
}

// 여러 장비의 요청을 받아 응답하는 서버 예.
// 장비는 임의 포트에서 보내지만 9001번 포트에서만 수신하므로 응답 포트를 9001로 지정함.
// 송신은 방화벽 정책에 맞춰 9100번 송신 전용 소켓을 사용함.
func ExampleEndpoint_server() {
	ep, err := udpnet.NewEndpoint(context.Background(), udpnet.Config{
		LocalAddr:    ":9000",
		SendBindAddr: ":9100",
	})
	if err != nil {
		log.Fatal(err)
	}
	ep.SetHandler(func(data []byte, src *net.UDPAddr) {
		ep.SendTo(data, &net.UDPAddr{IP: src.IP, Port: 9001})
	})
	ep.SetPanicHandler(func(src *net.UDPAddr, data []byte, r any, stack []byte) {
		log.Printf("panic from %s: %v\n%s", src, r, stack)
	})
	if err := ep.Start(); err != nil {
		log.Fatal(err)
	}
	defer ep.Shutdown()
}

// 멀티캐스트 그룹으로 장비 탐색 응답을 받는 예.
func ExampleEndpoint_multicast() {
	ep, err := udpnet.NewEndpoint(context.Background(), udpnet.Config{
		LocalAddr:          ":5353",
		MulticastGroup:     "239.255.0.1",
		MulticastInterface: "eth0",
	})
	if err != nil {
		log.Fatal(err)
	}
	ep.SetHandler(func(data []byte, src *net.UDPAddr) {
		log.Printf("discovered %s", src.IP)
	})
	if err := ep.Start(); err != nil {
		log.Fatal(err)
	}
	defer ep.Shutdown()

	// 브로드캐스트 송신 (SO_BROADCAST는 Go가 UDP 소켓에 기본으로 설정함)
	ep.SendTo([]byte("DISCOVER"), &net.UDPAddr{IP: net.IPv4bcast, Port: 5353})
}
