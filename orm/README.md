# Redis 데이터 구조

Redis의 주요 데이터 구조와 사용법을 정리한 문서입니다.

---

## String

가장 기본적인 데이터 타입으로, 텍스트, 숫자, 바이너리 데이터를 저장할 수 있습니다 (최대 512MB).

**주요 명령어**

```
SET key "value"
GET key
MSET key1 "value1" key2 "value2"
MGET key1 key2
SETNX key "value"
SETEX key 3600 "value"
INCR counter
INCRBY counter 10
DECR counter

```

**Go 예제**

```go
ctx := context.Background()
rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})

// 문자열 설정
rdb.Set(ctx, "session:token", "abc123", 0)

// TTL과 함께 설정
rdb.Set(ctx, "cache:user:1", "data", 1*time.Hour)

// 값 조회
val, _ := rdb.Get(ctx, "session:token").Result()

// 카운터 증가
rdb.Incr(ctx, "page:views")

```

**사용 사례**

- 캐시 저장
- 세션 데이터
- 카운터 (조회수, 좋아요 수)
- 토큰 저장
- 간단한 플래그

---

## List

순서가 있는 문자열 리스트로, 큐나 스택으로 사용 가능합니다.

**주요 명령어**

```
LPUSH mylist "first" "second"
RPUSH mylist "last"
LPOP mylist
RPOP mylist
LRANGE mylist 0 -1
LLEN mylist
LTRIM mylist 0 99
BLPOP mylist 5

```

**Go 예제**

```go
// 리스트에 추가 (큐처럼)
rdb.RPush(ctx, "queue:jobs", "job1", "job2")

// 요소 꺼내기
val, _ := rdb.LPop(ctx, "queue:jobs").Result()

// 블로킹 팝 (작업 큐에 유용)
result, _ := rdb.BLPop(ctx, 5*time.Second, "queue:jobs").Result()

// 범위 조회
items, _ := rdb.LRange(ctx, "notifications", 0, 9).Result()

// 최근 100개만 유지
rdb.LTrim(ctx, "logs", 0, 99)

```

**사용 사례**

- 작업 큐 (Task Queue)
- 최근 활동 피드
- 로그 저장 (최근 N개만 유지)
- 메시지 큐
- 스택/큐 구현

---

# Set

중복되지 않는 문자열의 정렬되지 않은 컬렉션입니다.

**주요 명령어**

```
SADD tags:post:1 "redis" "database" "nosql"
SMEMBERS tags:post:1
SISMEMBER tags:post:1 "redis"
SREM tags:post:1 "nosql"
SCARD tags:post:1
SINTER set1 set2
SUNION set1 set2
SDIFF set1 set2

```

**Go 예제**

```go
// 멤버 추가
rdb.SAdd(ctx, "tags:post:1", "redis", "database", "nosql")

// 모든 멤버 조회
members, _ := rdb.SMembers(ctx, "tags:post:1").Result()

// 멤버 존재 확인
exists, _ := rdb.SIsMember(ctx, "tags:post:1", "redis").Result()

// 교집합
inter, _ := rdb.SInter(ctx, "set1", "set2").Result()

// 합집합
union, _ := rdb.SUnion(ctx, "set1", "set2").Result()

```

**사용 사례**

- 태그 시스템
- 친구/팔로워 관계
- 중복 제거
- 블랙리스트/화이트리스트
- 온라인 사용자 추적

---

## Sorted Set (ZSet)

점수(score)로 정렬된 고유 문자열 집합입니다. 랭킹, 리더보드에 적합합니다.

**주요 명령어**

```
ZADD leaderboard 100 "player1" 200 "player2"
ZINCRBY leaderboard 50 "player1"
ZRANGE leaderboard 0 -1 WITHSCORES
ZREVRANGE leaderboard 0 9 WITHSCORES
ZRANGEBYSCORE leaderboard 100 200
ZSCORE leaderboard "player1"
ZRANK leaderboard "player1"
ZCARD leaderboard

```

**Go 예제**

```go
// 점수와 함께 추가
rdb.ZAdd(ctx, "leaderboard", redis.Z{
    Score: 100,
    Member: "player1",
}, redis.Z{
    Score: 200,
    Member: "player2",
})

// 점수 증가
newScore, _ := rdb.ZIncrBy(ctx, "leaderboard", 50, "player1").Result()

// 상위 10명 조회 (높은 점수부터)
top10, _ := rdb.ZRevRangeWithScores(ctx, "leaderboard", 0, 9).Result()
for _, z := range top10 {
    fmt.Printf("%s: %.0f\\n", z.Member, z.Score)
}

// 순위 조회
rank, _ := rdb.ZRevRank(ctx, "leaderboard", "player1").Result()

// 시간 기반 타임라인 (timestamp를 score로)
timestamp := float64(time.Now().Unix())
rdb.ZAdd(ctx, "timeline:user:1", redis.Z{
    Score: timestamp,
    Member: "event_data",
})

```

**사용 사례**

- 게임 리더보드
- 랭킹 시스템
- 시간순 타임라인
- 우선순위 큐
- 점수 기반 추천 시스템

---

## Hash

필드-값 쌍의 컬렉션으로, 객체를 저장하기에 적합합니다.

**주요 명령어**

```
HSET user:1000 name "John" age 30
HMSET user:1000 name "John" age 30 email "john@example.com"
HGET user:1000 name
HGETALL user:1000
HMGET user:1000 name email
HEXISTS user:1000 age
HDEL user:1000 email
HKEYS user:1000
HLEN user:1000
HINCRBY user:1000 age 1

```

**Go 예제**

```go
// Hash 설정
rdb.HSet(ctx, "user:1000", map[string]interface{}{
    "name": "John",
    "age": 30,
    "email": "john@example.com",
})

// 단일 필드 조회
name, _ := rdb.HGet(ctx, "user:1000", "name").Result()

// 전체 조회
user, _ := rdb.HGetAll(ctx, "user:1000").Result()
// user는 map[string]string

// 필드 삭제
rdb.HDel(ctx, "user:1000", "email")

```

**사용 사례**

- 사용자 프로필
- 세션 데이터
- 객체 저장
- 설정 관리
- 상품 정보

---

# Bitmap

비트 단위 연산이 가능한 문자열입니다. 플래그, 출석 체크 등에 유용합니다.

**주요 명령어**

```
SETBIT user:1000:login 0 1
GETBIT user:1000:login 0
BITCOUNT user:1000:login
BITOP AND result key1 key2
BITPOS user:1000:login 1

```

**Go 예제**

```go
// 날짜별 출석 체크 (day of year를 offset으로)
today := time.Now().YearDay()
rdb.SetBit(ctx, "attendance:user:1000:2025", int64(today), 1)

// 출석 확인
attended, _ := rdb.GetBit(ctx, "attendance:user:1000:2025", int64(today)).Result()

// 총 출석일 수
count, _ := rdb.BitCount(ctx, "attendance:user:1000:2025", nil).Result()

// 특정 기간 출석일 수
count, _ = rdb.BitCount(ctx, "attendance:user:1000:2025", &redis.BitCount{
    Start: 0,
    End: 30,
}).Result()

```

**사용 사례**

- 출석 체크
- 사용자 활동 플래그
- 실시간 분석
- 권한 관리
- A/B 테스팅 플래그

---

## HyperLogLog

카디널리티(고유 원소 개수) 추정에 사용하며, 매우 적은 메모리(최대 12KB)로 수십억 개의 고유 값을 추정할 수 있습니다.

**주요 명령어**

```
PFADD unique:visitors "user1" "user2" "user3"
PFCOUNT unique:visitors
PFMERGE result hll1 hll2 hll3

```

**Go 예제**

```go
// 일일 순 방문자 수 추정
key := fmt.Sprintf("visitors:%s", time.Now().Format("2006-01-02"))
rdb.PFAdd(ctx, key, "user123", "user456", "user789")

// 오늘 순 방문자 수
count, _ := rdb.PFCount(ctx, key).Result()
fmt.Printf("Today's unique visitors: %d\\n", count)

// 주간 순 방문자 수 (여러 HLL 병합)
days := []string{}
for i := 0; i < 7; i++ {
    date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
    days = append(days, fmt.Sprintf("visitors:%s", date))
}
weeklyCount, _ := rdb.PFCount(ctx, days...).Result()

```

**사용 사례**

- 고유 방문자 수 추정
- 고유 이벤트 카운팅
- 대규모 데이터의 중복 제거
- 실시간 통계 (메모리 효율적)

---

## JSON (RedisJSON 모듈)

JSON 데이터를 네이티브로 저장하고 조작할 수 있는 모듈입니다.

**주요 명령어**

```
JSON.SET user:1000 $ '{"name":"John","age":30,"address":{"city":"Seoul"}}'
JSON.GET user:1000
JSON.GET user:1000 $.name
JSON.SET user:1000 $.age 31
JSON.ARRAPPEND user:1000 $.hobbies '"reading"'
JSON.DEL user:1000 $.address
JSON.NUMINCRBY user:1000 $.age 1

```

**Go 예제**

```go
type User struct {
    Name    string  `json:"name"`
    Age     int     `json:"age"`
    Address Address `json:"address"`
}

user := User{Name: "John", Age: 30, Address: Address{City: "Seoul"}}

// JSON 직렬화 및 저장
userJSON, _ := json.Marshal(user)
rdb.Do(ctx, "JSON.SET", "user:1000", "$", string(userJSON))

// JSON 조회
result, _ := rdb.Do(ctx, "JSON.GET", "user:1000").Result()

// 역직렬화
var retrievedUser User
json.Unmarshal([]byte(result.(string)), &retrievedUser)

// 특정 경로 값 수정
rdb.Do(ctx, "JSON.SET", "user:1000", "$.age", 31)

```

**사용 사례**

- 복잡한 중첩 구조 저장
- 부분 업데이트가 필요한 객체
- JSON API 응답 캐싱
- 동적 스키마 데이터

---

## 데이터 구조 선택 가이드

**Hash vs JSON**

- Hash 사용: 단순한 객체, 필드별 개별 접근
- JSON 사용: 복잡한 중첩 구조, JSON 경로 기반 쿼리

**List vs Sorted Set**

- List 사용: 시간순 데이터, FIFO/LIFO 큐
- Sorted Set 사용: 점수 기반 정렬, 랭킹, 범위 조회

**Set vs Sorted Set**

- Set 사용: 순서 불필요, 집합 연산
- Sorted Set 사용: 순서/점수 필요, 범위 조회

**String**: 캐시, 세션, 카운터, 토큰 - O(1)
**List**: 작업 큐, 최근 활동 피드, 로그 - O(1) for push/pop
**Set**: 태그, 팔로워, 중복 제거 - O(1)
**Sorted Set**: 랭킹, 리더보드, 타임라인 - O(log N)
**Hash**: 객체 저장, 사용자 프로필 - O(1)
**Bitmap**: 출석 체크, 플래그, 실시간 분석 - O(1)
**HyperLogLog**: 고유 방문자 수 추정 - O(1)
**JSON**: 복잡한 중첩 구조, 동적 스키마 - O(1) ~ O(N)

---

## Tips

1. 키 네이밍 컨벤션: object-type:id:field (예: user:1000:profile)
2. TTL 설정: 캐시 데이터는 항상 만료 시간 설정
3. 메모리 관리: MEMORY USAGE key로 메모리 사용량 모니터링
4. 적절한 구조 선택: 데이터 접근 패턴에 맞는 구조 사용
5. 파이프라이닝: 다수의 명령어는 파이프라인으로 묶어서 실행