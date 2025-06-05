# oculus


### 构建工具

go install github.com/go-task/task/v3/cmd/task@latest

[task tab 补全设置](https://taskfile.dev/installation/#setup-completions)


### 代码提交自动增量 format 以及 CI 配置

依赖 direnv 和 Kitty

#### 安装

Mac:
```shell
brew tap ImSingee/kitty
brew install direnv ImSingee/kitty/kitty
```

Linux:
1. 自己安装 direnv 并配置 https://direnv.net/
2. 下载二进制放到 PATH 下 https://github.com/ImSingee/kitty/releases/download/v0.1.0-alpha.8/kitty-0.1.0-alpha.8-linux.amd64.tar.gz

#### 使用

安装好 direnv 和 kitty 后，在项目根目录初始化执行一次

```
kitty install
direnv allow
```

