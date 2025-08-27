# agents.md — sqsrouter 안내서

이 문서는 sqsrouter 코드베이스를 빠르게 이해하고 바로 사용할 수 있게 도와주는 실전 가이드야. AI 에이전트나 자동화 파이프라인이 SQS 메시지를 안정적으로 처리할 수 있도록 아키텍처, 사용법, 운영 팁을 한 번에 모았어. 출발해보자♪

- 대상 레포: github.com/HatsuneMiku3939/sqsrouter
- 핵심 구성요소: Consumer(SQS 폴링/삭제), Router(검증/디스패치), Handler(유저 정의 로직)

## 1) 개요
- 목적: AWS SQS에서 가져온 JSON 메시지를 타입/버전에 따라 핸들러로 라우팅하고, 스키마로 검증해서 일관된 처리 흐름을 제공해.
- 이점
  - 라우팅 규칙과 스키마 검증을 표준화
  - 실패/재시도/삭제 정책을 명확히
  - 동시성/타임아웃/그레이스풀 셧다운 패턴을 기본 제공

## 2) 아키텍처 개요
파이프라인은 이렇게 흘러가:
- Consumer: SQS Long Polling으로 메시지 배치 수신 → 각 메시지를 goroutine으로 처리
- Router: Envelope 스키마 검증 → 핸들러 조회 → (있다면) 페이로드 스키마 검증 → 핸들러 실행 → RoutedResult 반환
- Handler: 비즈니스 로직 실행 후 HandlerResult 반환(삭제 여부/에러 포함)

핵심 타입들:
- Consumer: NewConsumer(client, queueURL, router).Start(ctx)
- Router: NewRouter(envelopeSchema).Register(type, version, handler).RegisterSchema(type, version, schema).Route(ctx, raw)

## 3) 메시지 포맷과 스키마
Envelope는 라우팅/검증의 기준이야.

```json
{
  "schemaVersion": "1.0",
  "messageType": "UserCreated",
  "messageVersion": "v1",
  "message": { "userId": "123", "name": "Alice" },
  "metadata": { "timestamp": "2024-01-01T00:00:00Z", "source": "svcA", "messageId": "uuid-..." }
}
```

코드 내 Envelope 스키마(발췌):

```go
var EnvelopeSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "schemaVersion": { "type": "string" },
    "messageType": { "type": "string" },
    "messageVersion": { "type": "string" },
    "message": { "type": "object" },
    "metadata": { "type": "object" }
  },
  "required": ["schemaVersion", "messageType", "messageVersion", "message", "metadata"]
}`
```

- metadata.timestamp/messageId는 로그 상관관계에 좋아.
- metadata 파싱 실패해도 경고 로그 후 처리는 계속돼.

## 4) Router 사용법
```go
// Create router with envelope schema
router, err := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
if err != nil {
    panic(err) // (Do not panic in production; handle errors properly.)
}

// Register handler
router.Register("UserCreated", "v1", func(ctx context.Context, msgJSON []byte, metaJSON []byte) sqsrouter.HandlerResult {
    // Parse msgJSON to struct and process
    // return ShouldDelete=true for permanent outcomes; false to retry
    return sqsrouter.HandlerResult{ShouldDelete: true, Error: nil}
})

// (Optional) Register payload schema for validation
router.RegisterSchema("UserCreated", "v1", `{
  "$schema":"http://json-schema.org/draft-07/schema#",
  "type":"object",
  "properties":{ "userId":{"type":"string"}, "name":{"type":"string"} },
  "required":["userId","name"]
}`)
```

Route 동작 단계:
1) Envelope 스키마 검증(구조 불량 시 영구 실패로 delete)
2) Envelope 언마샬
3) 핸들러 조회(없으면 영구 실패로 delete)
4) 페이로드 스키마 검증(등록된 경우, 실패 시 delete)
5) 메타데이터 파싱(실패해도 경고만)
6) 핸들러 실행 → HandlerResult 수집

핵심 계약:
- HandlerResult.ShouldDelete
  - true: 처리 완료(성공 또는 영구 실패)로 간주해 삭제
  - false: 일시 오류로 간주해 재시도 유도(가시성 타임아웃 경과 후 재전달)
- HandlerResult.Error
  - nil: 성공
  - non-nil: 실패(로그와 함께 위 정책에 따라 삭제/재시도)

## 5) Consumer 사용법
설정 상수(요약):
- maxMessages=5, waitTimeSeconds=10(Long Polling), processingTimeout=30s, deleteTimeout=5s, retrySleep=2s

```go
// Build AWS SQS client (aws-sdk-go-v2) and create Consumer
client := sqs.NewFromConfig(cfg)
consumer := sqsrouter.NewConsumer(client, "https://sqs.{region}.amazonaws.com/{account}/{queue}", router)

// Start polling (blocking until ctx canceled)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
consumer.Start(ctx)
```

처리/삭제 흐름:
- Router.Route 결과의 HandlerResult.ShouldDelete가 true면 DeleteMessage 호출
- false면 삭제하지 않고 재시도되도록 남겨둬(가시성 타임아웃 만료 후 재배달됨)

그레이스풀 셧다운:
- Start 루프는 ctx 취소를 감지하면 새로운 폴링을 멈추고
- inflight goroutine이 끝날 때까지 기다려줘

## 6) 빠른 시작 예제
레포의 example/basic을 참고하면 가장 빨라.

경로: example/basic/main.go

```go
// Pseudocode summary (refer to the real example in the repo)
router, _ := sqsrouter.NewRouter(sqsrouter.EnvelopeSchema)
router.Register("UserCreated", "v1", userCreatedHandler)
router.RegisterSchema("UserCreated", "v1", userCreatedSchemaJSON)

client := sqs.NewFromConfig(cfg)
consumer := sqsrouter.NewConsumer(client, queueURL, router)
consumer.Start(ctx)
```

로컬에서 e2e 테스트를 하고 싶다면 test/docker-compose.yaml 및 test/e2e.sh를 참고해. LocalStack 등으로 SQS 대체 환경을 구성할 수 있어.

## 7) 운영 가이드
동시성/타임아웃 튜닝:
- maxMessages: 폴링당 가져오는 개수. 처리량과 메모리 사이에서 균형
- waitTimeSeconds: Long Polling 시간. 비용/빈응답 감소에 도움
- processingTimeout: 핸들러 처리 시간 상한. K8s 종료 기간보다 짧게 유지 추천
- deleteTimeout: 삭제 API 호출 타임아웃
- SQS Visibility Timeout과 processingTimeout의 관계를 고려해서 설정해야 해

장애 유형별 처리:
- 영구 실패(예: 잘못된 페이로드 스키마, 등록되지 않은 핸들러): ShouldDelete=true로 삭제. DLQ를 반드시 설정해 모니터링하자
- 일시 오류(예: 외부 의존성 일시 장애): ShouldDelete=false로 재시도 유도. 재시도 폭주 방지를 위해 지표/알람 구성 추천

로그/관측성:
- 성공/실패 로그에 Timestamp/MessageID/Type/Version을 남겨 추적이 쉬워
- metadata 파싱에 실패해도 경고만 뜨고 처리는 진행돼. 필요 시 메타데이터 필수화는 스키마로 제어할 수 있어

보안/권한:
- SQS 권한은 최소 권한 원칙으로 설정해(ReceiveMessage/DeleteMessage)
- 메시지에 민감정보가 담기면 암호화/KMS 정책도 확인해

## 8) 테스트/CI
- 유닛 테스트: consumer_test.go, router_test.go
- e2e: test/e2e.sh, test/docker-compose.yaml
- GitHub Actions에서 lint/test/e2e가 병렬로 동작해. 단순 문서 변경은 영향이 거의 없어

로컬에서 문제 생기면:
- go 모듈 종속성 문제: go mod tidy 로 정리하면 될 수 있어.

## 9) 확장과 베스트 프랙티스
- 메시지 타입/버전 전략: messageType+messageVersion으로 라우팅 키가 만들어져. version 업그레이드 시 Register/Schema를 함께 추가해서 점진적으로 전환해
- 스키마 진화: required 필드 최소화로 초기 도입을 쉽게 하고, backward-compatible 변경을 고려해
- 멱등성: 외부 부작용(예: DB write) 전후로 idempotency key를 도입하면 중복 처리에 안전해
- 추적성: messageId를 분산 추적/로그 상관관계 키로 사용하자

## 10) FAQ
- 핸들러가 없으면 어떻게 돼?
  - 해당 키(type:version) 미등록이면 영구 실패로 delete되고 에러가 기록돼
- metadata 파싱이 실패하면?
  - 경고 로그만 남기고 처리는 계속돼
- 재시도는 어떻게 동작해?
  - ShouldDelete=false면 삭제하지 않아. 가시성 타임아웃이 지나면 다시 큐로 나타나고 컨슈머가 재수신해

## 11) 참고 경로
- Router/Schema/Route: router.go
- Consumer/Start/processMessage: consumer.go
- 예제: example/basic/main.go
- 테스트: consumer_test.go, router_test.go
- e2e: test/e2e.sh, test/docker-compose.yaml
