# serialport ( tcpnet과 동일 )

> `github.com/Jaeun-Choi98/modules/serialport`

스트림 통신 환경에서 웹 프레임워크와 유사한 개발 경험을 제공하기 위한 Go 모듈입니다.  
애플리케이션 3계층 아키텍처(표현·서비스·DB)에 통합하여 사용할 수 있으며,  
스트림 특성상 발생하는 단편화(fragmentation), 병합(coalescing) 등의 변수에 일관성 있게 대응합니다.

---

## 패키지 구성

```
tcpnet/
├── basic/
│   └── terminal/ # 통신 관리 및 연결
└── advanced/
    ├── parser/   # 프로토콜별 바이트 스트림 파싱 인터페이스
    ├── model/    # 세션 컨텍스트 (파싱 결과, 응답 채널 등)
    └── handler/  # 패킷 식별 기반 처리 로직 분배
```

### Basic 패키지

통신의 기본 역할을 담당합니다.

| 패키지 | 역할 |
|--------|------|
| `basic/serialport` | 장치 오픈 생성, 고루틴 시작/종료 |


### Advanced 패키지

스트림 환경에서 일관된 처리 구조를 제공하기 위한 패키지들로, 아래 의존 방향을 따릅니다.

```
[Raw Bytes Stream]
       │
       ▼
   parser.Parser          ← 프로토콜 정의에 따라 바이트를 파싱
       │
       ▼
   model.Context          ← 파싱된 데이터 + 응답 채널을 세션 단위로 보관
       │
       ▼
  handler.Handler         ← 패킷 종류에 따라 처리 함수로 분배 (라우팅)
       │
       ▼
  [비즈니스 로직 / 서비스 계층]
```

| 패키지 | 역할 |
|--------|------|
| `advanced/parser` | `Parser` 인터페이스 제공. 프로토콜마다 구현체를 작성해 스트림을 파싱 |
| `advanced/model` | 세션 컨텍스트. 파싱된 요청 데이터, 응답 채널, 메타정보를 저장·조회 |
| `advanced/handler` | 패킷 식별자(커맨드·타입 등)를 키로 처리 함수를 등록하고 분배 |
