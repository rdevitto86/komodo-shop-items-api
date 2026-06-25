# TODO

> **Current Version:** V1

## V1 (Current)

> Status: Fully implemented. S3-backed product catalog works. Suggestions use real inventory-based ranking. Repair routes live here pending relocation decision.

### OpenAPI

- **[M]** Complete `openapi.yaml` — required by downstream consumer codegen (cart-api, statistics-api, order-api cross-reference)

### Open Items

- **[M]** Relocate `/services/repair` routes to `komodo-order-reservations-api` — repair booking is a time-slot/appointment concern, not a catalog concern; shop-items-api should only expose repair *listings* (`service_type=repair`); the booking flow belongs in reservations-api; coordinate the route split before implementing either handler
- **[M]** Add `GET /v1/suggestions` — recommendation endpoint consumed by UI on product detail + cart pages; query params: `item_id` (optional anchor), `user_id` (optional), `limit`. Ranking source TBD (inventory-popularity, statistics-api co-purchase, or both); document the ranking strategy in `docs/data-model.md`. _Options: in-process ranking on shop-items-api · delegate to `komodo-statistics-api` and proxy · separate `komodo-recommendations-api` (heavier; only if ranking grows beyond simple co-purchase)._

## Audit Findings — 2026-05-17

- **[H]** **`FetchAllProducts` / `FetchAllServices` are N+1 S3 GETs.** `internal/store/store.go:56-96` reads `inventory.json` for the SKU list then issues one `GetObject` per SKU sequentially. `GetSuggestions` calls `FetchAllProducts` + `FetchInventory` on **every request** with no cache (`internal/handlers/suggestion.go:65,74`). At a 200-SKU catalog this is ~200 sequential S3 round-trips per suggestion call. Three fixes, ordered by impact: 1) bounded-parallelism fan-out (errgroup with `Concurrency = 16`); 2) in-process LRU cache keyed by `bucket+key` with short TTL (60s) — catalog rarely changes; 3) denormalize: embed product summary fields into `inventory.json` so listing endpoints serve from one GET. Pick 2 + 3; reserve 1 for cold-cache fan-out.
- **[M]** **`GetItemBySKU` does up to 2 S3 GETs.** `internal/handlers/item.go:35-46` tries `products/{sku}.json` then falls back to `services/{sku}.json` — services pay the cost of a failed S3 GET on every read. The inventory manifest already carries the type per SKU; resolve type from the cached inventory and issue exactly one GET.
- **[M]** **No CDN / response cache headers on catalog reads.** `GetItemBySKU` and `GetInventory` are public, infrequently changing, and ideal CloudFront candidates. Set `Cache-Control: public, max-age=60, stale-while-revalidate=300` (or similar) and `ETag` from the S3 object's `ETag`. Drops origin load and tail latency for cold catalog browsers.
- **[M]** **`GetSuggestions` exclusion set has a partial bug.** `internal/handlers/suggestion.go:50-53` sizes `excluded` for `len(reqBody.Exclude)+len(reqBody.SKUs)` but only iterates `reqBody.Exclude`. Either the `SKUs` field (presumably current-cart / anchor SKUs) is intended to also be excluded — current code silently does not exclude them — or the size hint is stale. Decide intent, fix one or the other.
- **[L]** **Per-call `os.Getenv(S3_ITEMS_BUCKET)`.** Read once at startup into a package var (or, better, injected into a handler struct as part of the instance-based-design sweep). Trivial latency cost, but it's also the only thing standing between this service and trivial DI.
- **[L]** **`store.S3Client` is a package-level global.** Same antipattern as auth-api / user-api / order-api; defer to the cross-SDK refactor item.
- **[L]** **No body-size limit on `POST /v1/item/suggestion`.** `RuleValidationMiddleware` may or may not bound the body; confirm and add an explicit `MaxBytesReader` if not. Cheap defense against accidental large payloads.
- **[M]** Wire `GET /health/ready` in `cmd/public/main.go` — checkers: `S3Checker("items-bucket", s3client, os.Getenv("S3_ITEMS_BUCKET"))`; blocked on forge SDK `api/handlers/health` release and `S3Checker` built-in shipping (tracked in forge SDK TODO `api/handlers/health`)

## Testing

- **[M]** **Implement CI test stack** — add `github.com/stretchr/testify` and `go.uber.org/mock` to `go.mod`; generate mocks from the store interface via `mockgen -source`; convert stub `*_test.go` files to real unit tests (table-driven, `t.Run` subtests) with `net/http/httptest` for handler layer; add `testutil.Component(t)` / `testutil.Integration(t)` tier decorators from the SDK (`github.com/rdevitto86/komodo-forge-sdk-go/testing/testutil`, `TEST_TIER`-gated; default tier is `unit`); add `testcontainers-go` for integration tests against S3 (LocalStack) once caching lands; apply section banners. Reference auth-api as the canonical pattern once its retrofit is complete.

## Audit findings — gaps from audit (follow-up)

> The 2026-05-17 findings above are re-verified against current code and all still valid. Additional standards/correctness items below.

### Convert colon-prefixed errors and fix log-message construction
**Problem:** `internal/store/store.go` wraps every error with a `store.FuncName:` prefix (`"store.FetchProductBySKU: %w"`, etc. — 13 occurrences) — hard-rule violation (principles §1). Worse, the warn logs build the message by string concatenation with the same prefix and the raw error inlined: `logger.Warn("store.FetchAllServices: skipping sku " + item.SKU + ": " + err.Error())` (store.go:67,90) and `suggestion.go:76`. That both violates the verb-phrase rule and defeats structured logging.
**Action:** Rewrite wraps as `"failed to fetch product %s: %w"`. Rewrite logs as `logger.Warn("skipping malformed catalog object", logger.Attr("sku", item.SKU), logger.AttrError(err))`.

### Remove the package doc block and name-leading comments
**Problem:** `internal/store/store.go:1-4` carries a `// Package store fetches...` doc block — a comments.md hard violation. Doc comments across `store.go` and the handlers open with the identifier name (`// FetchProductBySKU retrieves...`, `// GetSuggestions returns...`), and `GetSuggestions` (suggestion.go:23-30) is a multi-paragraph block.
**Action:** Delete the package block; reword doc comments verb-leading and trim to ≤2 sentences.

### Eliminate the duplicate inventory fetch per suggestion call
**Problem:** `GetSuggestions` calls `store.FetchAllProducts` (which itself calls `FetchInventory`, store.go:80) and then calls `store.FetchInventory` again directly (suggestion.go:74) — `inventory.json` is read from S3 twice on every suggestion request, on top of the N+1 product GETs already tracked.
**Action:** Fetch inventory once and pass it into the products listing (or return both from a single combined call). Folds naturally into the planned catalog cache.
