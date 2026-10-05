# expression-diff-testing-go

记录过滤表达式引擎的**差分测试**框架。引擎有两条求值路径：

- **解释路径** `expr.Eval`：逐条记录解释执行，作为参考语义。
- **批量路径** `expr.EvalBatch`：面向批量数据的优化路径，做向量化短路
  （右操作数只为结果未定的记录求值）与常量折叠。

`difftest` 包用固定种子生成用例，让两条路径对同一输入分别求值并比较，
发现分歧后自动收缩成最小复现用例，并对分歧归因。

## 快速开始

```go
rep := difftest.Run(context.Background(), difftest.Config{
    Seed:        42,
    NumCases:    1000,
    Shards:      4,
    Concurrency: 8,
    Budget:      30 * time.Second,
})
for _, d := range rep.Divergences {
    fmt.Println(d.CaseID, d.Kind, d.Min)
}
```

## 等价语义

两条路径的输出按以下规则判定等价（`difftest.Equivalent`），本来等价的
输出不会被误判为分歧：

- **浮点容差**：`float64` 结果在 `FloatTol` 绝对容差内视为相等；
  NaN 只与 NaN 等价。
- **空值语义**：`nil` 只等于 `nil`；`nil` 不等于 `0.0`、`false` 或 `""`。
- **集合/分组无序性**：`[]any` 结果按无序多重集比较，元素顺序不敏感，
  元素比较同样应用浮点容差与空值规则。
- **错误分类**：错误按 `expr.ErrKind`（未知字段、类型不匹配、除零、
  空值运算、步数超限）比较，错误消息文本不参与等价判定。

## 分歧归因

`difftest.DivKind` 把分歧分成可区分的类别，不会混成同一种失败：

| 类别 | 含义 |
|---|---|
| `DivValue` | 两边都返回了值，但值不等价 |
| `DivErrorVsValue` | 一边报错，另一边返回了值 |
| `DivErrorKind` | 两边都报错，但错误类别不同 |
| `DivTimeout` | 恰好一边超时 |
| `DivCrash` | 恰好一边 panic |

两边以同样方式失败（都超时、都崩溃、同类错误）不视为分歧。

## 配置项

| 字段 | 默认 | 说明 |
|---|---|---|
| `Seed` | 0 | 随机种子。同一 `Seed` 与相同配置产出逐条一致的用例序列与判定结论；用例 i 只由 `(Seed, i)` 决定，与生成顺序、分片、调度无关 |
| `NumCases` | 100 | 生成的用例数 |
| `MaxDepth` | 6 | 表达式嵌套深度上界（资源上界） |
| `MaxRecords` | 16 | 单用例记录数上界（资源上界） |
| `FloatTol` | 1e-9 | 浮点等价绝对容差 |
| `Shards` | 1 | 分片数；用例 i 固定属于分片 `i % Shards` |
| `Concurrency` | 1 | 每个分片的 worker 数 |
| `CaseTimeout` | 2s | 单条用例、单侧路径的超时；超时后该侧标记为超时，不留下半成品状态，也不影响后续用例 |
| `Budget` | 无 | 整体运行预算；到期后停止派发新用例，返回已收集的结论（`Report.Stopped=true`） |
| `MaxSteps` | 无 | 单次求值的步数上界；超限报 `ErrStepLimit`。注意：设置步数上界时批量路径会停用常量折叠，以保证两条路径的步数账目一致 |

## 并发与隔离

- 两条路径各自在独立 goroutine、独立超时上下文中求值：慢的一侧不会
  阻塞、拖垮或污染另一侧；一侧 panic 被就地回收为 `DivCrash`。
- 分片大小、并发度与调度顺序不改变任何等价判定：结果按用例 ID 顺序
  汇总。
- 确认分歧后只保留收缩出的最小用例（`Divergence.Min`），原始输入立即
  释放。

## 用例收缩

`difftest.Shrink` 按固定顺序（记录按 delta-debugging 分块、字段按名字
排序、表达式按广度优先子表达式）贪心收缩，结果只取决于输入用例本身，
与探索顺序无关；收缩后的用例必然仍触发同一 `DivKind`。

## 测试

```sh
go test ./...        # 单元与差分测试
go test -race ./...  # 含竞态检测
```

覆盖：种子复现、等价判定边界、用例收缩（最小性/确定性/稳定性）、
并发一致性、超时与取消、深层输入的资源上界，以及两条路径在多组
种子下的全量一致性。
