# modules 저장소 태그 관리 가이드

저장소 루트: `modules/` (repo path: `github.com/Jaeun-Choi98/modules`)
구조: 루트 아래 `utils/`, `tcpnet/`, `sse/`, `shell/`, `serialport/`, `orm/`, `mom/`, `eventbus/` 등 여러 모듈 디렉토리 존재.

---

## 1. 태그 네이밍 규칙

| 대상 | 태그 형식 | 예시 |
|---|---|---|
| 루트(전체) 릴리즈 | `vX.Y.Z` | `v1.0.0` |
| 개별 모듈 릴리즈 | `<모듈명>/vX.Y.Z` | `utils/v1.0.0`, `tcpnet/v1.0.0` |

이 네이밍은 임의 관례가 아니라 **Go 멀티모듈 모노레포 공식 컨벤션**이다 (아래 4번 참고). 각 모듈에 독립된 `go.mod`이 있고, `module github.com/Jaeun-Choi98/modules/utils` 처럼 선언돼 있어야 이 방식이 정상 동작한다.

---

## 2. 특정 모듈만 수정 후 minor 버전 올리기

예: `utils` 모듈 수정, `v1.0.0` → `v1.1.0`

```bash
# 1. 모듈 수정 후 커밋 (repo root는 modules/)
git add utils/
git commit -m "utils: add feature X"
git push origin main

# 2. 해당 모듈의 최신 태그 확인
git tag -l "utils/v*" --sort=-v:refname

# 3. 새 태그 생성 (annotated tag 권장)
git tag -a utils/v1.1.0 -m "utils v1.1.0"

# 4. 태그 푸시
git push origin utils/v1.1.0

# (선택) GitHub Release 생성
gh release create utils/v1.1.0 --title "utils v1.1.0" --notes "..."
```

**포인트**

- 태그는 커밋 시점을 가리키므로, 변경 사항을 커밋한 **뒤에** 그 커밋에 태그를 찍어야 한다.
- git 자체는 디렉토리와 태그를 자동 연결해주지 않는다 — `utils/` prefix는 순수 네이밍 규칙이며, 실제로는 어떤 커밋이든 가리킬 수 있다. 개발자가 "이 태그는 utils 모듈 릴리즈다"라는 의미로 이름을 그렇게 짓는 것뿐이다.
- 건드리지 않은 다른 모듈(tcpnet, sse 등)의 태그는 그대로 둔다.

---

## 3. `go get`으로 받아오기

```bash
# 특정 버전
go get github.com/Jaeun-Choi98/modules/utils@v1.1.0

# 최신 커밋 (태그 없이)
go get github.com/Jaeun-Choi98/modules/utils@latest

# 루트 모듈(별도 go.mod가 있는 경우)
go get github.com/Jaeun-Choi98/modules@v1.0.0
```

module path에서 접두사(`utils/`)는 태그 매칭 시 자동으로 처리되므로, `@` 뒤에는 접두사 없는 순수 버전만 쓴다.

---

## 4. Go가 모노레포 서브모듈을 처리하는 방식

Go는 **디스크상 디렉토리 위치**가 아니라 **module path 문자열 + 태그 prefix 매칭**으로 서브모듈을 구분한다.

### 4-1. 저장소 루트(repo root) 판별

`github.com`, `gitlab.com` 등 잘 알려진 호스팅은 규칙이 고정돼 있어, module path의 앞 3개 세그먼트(`host/user/repo`)를 repo root로 간주한다.

```
module path : github.com/Jaeun-Choi98/modules/utils
repo root   : github.com/Jaeun-Choi98/modules       ← 앞 3세그먼트
subdir      : utils                                  ← 나머지
```

(잘 알려지지 않은 호스트는 해당 URL의 `go-import` meta 태그를 읽어 repo root를 알아낸다.)

### 4-2. 태그 매칭

repo root를 clone/fetch한 뒤, 남은 서브경로(`utils`)를 prefix로 갖는 태그를 찾는다. `@v1.1.0` 요청 시 `utils/v1.1.0` 태그를 탐색. annotated/lightweight 태그 여부는 상관없이 순수 문자열 prefix 매칭이다.

### 4-3. 우선순위: 가장 긴 prefix 우선

저장소에 `v1.1.0`(루트용)과 `utils/v1.1.0`(서브모듈용)이 동시에 있어도, `.../modules/utils` 모듈 요청 시엔 **더 구체적인(prefix가 긴)** `utils/v1.1.0`이 선택된다. 이 덕분에 여러 모듈이 서로 독립적으로 버전 관리된다.

### 4-4. 메이저 버전 2 이상

- `go.mod`의 module 선언에 `/v2` 접미사 필요: `module github.com/.../modules/utils/v2`
- 태그도 `utils/v2.0.0` 형태로 찍어야 함

### 4-5. 적용 범위

이 로직은 `go get`이 VCS를 직접 볼 때뿐 아니라, `proxy.golang.org` 같은 모듈 프록시가 백엔드에서 모듈을 캐싱할 때도 동일하게 적용된다.

---

## 5. 체크리스트

- [ ] 모듈별 `go.mod`의 module path가 실제 서브디렉토리 경로와 정확히 일치하는지 확인
- [ ] 릴리즈 전 변경 사항 커밋 → 태그는 그 커밋에 찍기
- [ ] 태그 이름 충돌(같은 버전을 다른 모듈에 실수로 찍는 등) 주의
- [ ] 메이저 버전 올릴 때는 module path `/vN` 접미사와 태그 `/vN.0.0` 둘 다 갱신
