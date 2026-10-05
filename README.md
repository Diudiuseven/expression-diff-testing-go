# expression-diff-testing-go

记录过滤表达式引擎，内置**双求值路径**与**差分测试框架**：

- `expr`：表达式引擎。同一棵 AST 有两条求值路径——
  - `expr.Eval`：逐条记录的解释执行路径（参考实现）；
  - `expr.EvalBatch`：批量/列式求值路径，带逻辑短路行裁剪（优化实现）。
- `difftest`：差分测试框架，用确定性随机器生成用例，在两条路径上分别
  求值并比较，对分歧做归因、收缩与并发执行。

## 表达式语义速览

- 类型：null、bool、int、float、string、list。
- 逻辑运算（`and`/`or`/`not`）采用 SQL 三值逻辑：`false AND x = false`、
  `true OR x = true` 会短路，右操作数的错误不会被观察到。
- 比较与算术中 null 传播（结果为 null）；int/float 混合数值比较与运算
  自动提升；除以零产生 `ErrDivByZero`。
- 错误按类别分类（`ErrType`、`ErrUnknownField`、`ErrDivByZero`、
  `ErrArity`、`ErrBudget`），等价判定只比较类别，不比较消息文本。

## 等价语义（Equivalence）

`difftest.Equivalent` 判定两条路径的输出等价，以下差异**不**构成分歧：

- **浮点容差**：`|a-b| <= FloatTol * max(1, |a|, |b|)`（绝对+相对混合），
  由 `EquivOptions.FloatTol` 控制，默认 `1e-9`；int 与 float 按数值比较。
- **空值语义**：null 仅与 null 等价，null ≠ false。
- **集合/分组无序性**：list 结果按无序多重集比较（递归应用容差），
  元素顺序不同不算分歧，但元素基数不同算分歧。
- **错误分类**：两侧都报错时，仅当 `ErrClass` 相同才等价，消息文本无关。

## 分歧归因（DivKind）

每类分歧单独报告，绝不混合：

| 类别 | 含义 |
|---|---|
| `value-mismatch` | 两侧都返回值但取值不同 |
| `value-vs-error` | 一侧返回值、另一侧报错 |
| `error-class-mismatch` | 两侧都报错但错误类别不同 |
| `timeout` | 一侧超过单条用例时间预算 |
| `crash` | 一侧发生 panic |

## 配置项

### 生成 `GenConfig`

| 字段 | 默认 | 说明 |
|---|---|---|
| `Seed` | — | 随机种子；同种子同配置产出逐条一致的用例序列与结论 |
| `NumCases` | 100 | 用例数 |
| `MaxDepth` | 5 | 表达式最大嵌套深度（资源上界） |
| `MaxRecords` | 8 | 每条用例最大记录数 |
| `MaxListLen` | 4 | list 值最大长度 |

### 运行 `RunConfig`

| 字段 | 默认 | 说明 |
|---|---|---|
| `ShardSize` | 16 | 每个分片的用例数；不影响判定结果 |
| `Workers` | 4 | 并发 worker 数；不影响判定结果 |
| `CaseTimeout` | 2s | 单条用例单侧求值超时；超时记为 `timeout` 分歧 |
| `Budget` | 30s | 整体墙钟预算；到期即停，返回已收集结论（`Report.Stopped=true`） |
| `Equiv.FloatTol` | 1e-9 | 浮点容差 |

## 运行保证

- **可复现**：同一种子与配置产出完全一致的用例序列；分歧报告按用例
  ID 排序，与分片大小、并发度、调度顺序无关。
- **隔离**：每条用例的每一侧求值都在独立 goroutine 中运行并带 panic
  恢复，慢的一侧不会阻塞、拖垮或污染另一侧；取消/超时后不留半成品
  状态，后续用例不受影响。
- **资源上界**：用例是流式生成的（不预生成全部用例）；表达式深度、
  记录数、list 长度、求值步数（`MaxSteps`）均有上界；确认分歧后只保留
  收缩出的最小用例，原始输入被丢弃。
- **收缩**：`Shrinker` 按固定顺序（删记录 → 删字段 → 字段置 null →
  子表达式化简）迭代到不动点，每次化简都重新验证仍触发**同一类**
  分歧；结果确定且与探索顺序无关。

## 示例

```go
rep := difftest.Run(context.Background(), difftest.RunConfig{
    Gen: difftest.GenConfig{Seed: 42, NumCases: 1000, MaxDepth: 6},
    Equiv: difftest.EquivOptions{FloatTol: 1e-9},
    Workers: 8, ShardSize: 16,
    CaseTimeout: 2 * time.Second,
    Budget:      60 * time.Second,
})
for _, d := range rep.Divergences {
    fmt.Printf("case %d: %s  minimal expr: %s\n", d.Case.ID, d.Kind, d.Case.Expr)
}
```

## 测试

```sh
go test ./...          # 单元 + 差分 + 并发一致 + 超时取消
go test ./... -race    # 竞态检测
```
