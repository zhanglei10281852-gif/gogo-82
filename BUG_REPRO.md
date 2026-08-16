# Bug Reproduction

## 包的性质

当前 test_model_fix 保存的是被测模型修复后的结果源码，不是初始含 Bug 源码。要复现原始缺陷，必须检出下面固定的 parent SHA；不要在当前修复结果源码上期待重新出现修复前失败。生成系统使用的可信验证补丁和完整验证日志仅在本地留存，不提交到结果分支。

## 问题现象

撤离演练的报告读起来自相矛盾。下发撤离指令后，两名在册人员里只有一个在集合点刷了卡，personnel 段落明确写着 muster complete=false、1 unaccounted，还逐条列出了那个人最后出现在哪个工作面、几点几分；可同一份报告的总体结论却是正面的，verdict 只写“point(s) usable、no trip、ventilation adequate”，一个字都没提没清点到的人，mineguard report 的退出码也是 0。当时瓦斯、通风、闭锁、作业票、巡检都是干净的，只有清点没完成。我们的调度脚本就是靠这个退出码决定要不要拉响后续流程的，现在少一个人也会被当成一切正常放过去。请修复总体结论的判定，让未清点人员参与结论，同时保持瓦斯跳闸数、闭锁跳闸数、通风是否达标、作业票拒批数、巡检超期数各自的口径与 headline 的拼装顺序不变，并保证全量测试通过。

## 含 Bug 版本

- 仓库：zhanglei10281852-gif/gogo-82
- 仓库地址：https://github.com/zhanglei10281852-gif/gogo-82.git
- parent SHA：78ecda9ecb465b4f56e77ca06246254d8b0bb436

## 复现步骤

```bash
git clone -- https://github.com/zhanglei10281852-gif/gogo-82.git bug-repro
cd bug-repro
git checkout --detach 78ecda9ecb465b4f56e77ca06246254d8b0bb436
go test ./internal/pipeline -run "^TestUnaccountedPersonMakesTheMineUnsafe$" -count=1 -v
```

## 双架构完整错误信息

### linux/amd64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/pipeline -run "^TestUnaccountedPersonMakesTheMineUnsafe$" -count=1 -v
=== RUN   TestUnaccountedPersonMakesTheMineUnsafe
    muster_test.go:48: a mine with an unaccounted person is not safe: "3 point(s) usable of 3, no trip, ventilation adequate"
--- FAIL: TestUnaccountedPersonMakesTheMineUnsafe (0.00s)
FAIL
FAIL	MineGuard/internal/pipeline	0.003s
FAIL

```

stderr：

```text
warning: internal/pipeline/muster_test.go has type 100755, expected 100644
warning: internal/pipeline/muster_test.go has type 100755, expected 100644

```

### linux/arm64

- 容器内复现预期退出码：1
- 容器内复现实际退出码：1

stdout：

```text
$ go test ./internal/pipeline -run "^TestUnaccountedPersonMakesTheMineUnsafe$" -count=1 -v
=== RUN   TestUnaccountedPersonMakesTheMineUnsafe
    muster_test.go:48: a mine with an unaccounted person is not safe: "3 point(s) usable of 3, no trip, ventilation adequate"
--- FAIL: TestUnaccountedPersonMakesTheMineUnsafe (0.11s)
FAIL
FAIL	MineGuard/internal/pipeline	0.307s
FAIL

```

stderr：

```text
warning: internal/pipeline/muster_test.go has type 100755, expected 100644
warning: internal/pipeline/muster_test.go has type 100755, expected 100644

```

## 通过条件

撤离指令下发后仍有 1 名在册人员未清点、而瓦斯跳闸数=0、闭锁跳闸数=0、通风达标、作业票拒批数=0、巡检超期数=0 时，Summary.Safe 为 false 且 headline 中包含未清点人员的描述；同一份数据在最后一人到达集合点后 Safe 恢复为 true；points/usable_points 统计、gas_worst_level、跳闸传导、report 与各子命令的 0/1/2 退出码语义、headline 各分句的稳定排序等既有行为不回归；定向测试、全量 go test ./... -count=1 与 go build ./... && go vet ./... 全部通过；校准与远端复跑均在 golang:1.22 linux/amd64 单架构完成。
