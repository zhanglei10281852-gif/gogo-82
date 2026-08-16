# BENZHI_README

## 项目说明

- 项目：zhanglei10281852-gif/gogo-82
- 项目用途：MineGuard is a self-contained command line tool that reads an underground coal mine monitoring dataset and turns it into a deterministic assessment: which sensor readings may be trusted, what the atmosphere at each monitoring point looks like, whether the ventilation network is conserved and adequate, which controlled circuits may stay energised, who is underground, which high risk work permits may be honoured, and which scheduled inspections are overdue.
- Go 工具链：`golang:1.22`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/mineguard

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-82-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-82-arm64 linux/arm64
docker run -it benzhi-task-82-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-82-arm64:latest
```

## 题目验证命令

1. 预期退出码 0：`go test ./internal/pipeline -run "^TestUnaccountedPersonMakesTheMineUnsafe$" -count=1 -v`
2. 预期退出码 0：`go test -buildvcs=false -count=1 ./...`
3. 预期退出码 0：`go build ./... && go vet ./...`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
