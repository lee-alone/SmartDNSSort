# SmartDNSSort

SmartDNSSort is a high-performance, intelligent DNS proxy server designed to optimize your internet experience by providing the fastest DNS query responses while also blocking advertisements and malicious domains. 
SmartDNSSort 是一个高性能的智能 DNS 代理服务器，旨在通过提供最快的 DNS 查询响应来优化您的互联网体验，同时还能拦截广告和恶意域名。

It achieves this by concurrently querying upstream DNS servers, measuring the response times of the returned IP addresses, and returning a sorted list of the fastest IPs.
它通过并发查询上游 DNS 服务器，测量返回的 IP 地址的响应时间，并返回一个按速度排序的 IP 列表来实现这一功能。

It also includes a powerful, regex-capable ad-blocking engine and multiple layers of caching to ensure both speed and efficiency.
它还包含一个功能强大、支持正则表达式的广告拦截引擎和多层缓存机制，以确保速度和效率。

## Features / 功能特性

*   **Fastest IP Sorting:** Queries multiple upstream DNS servers and pings the returned IP addresses to find and return the fastest ones first.
    **最快 IP 排序：** 查询多个上游 DNS 服务器并对返回的 IP 地址进行 ping 测试，以找到并优先返回最快的 IP。

*   **Powerful Ad-blocking:** Built-in ad-blocker with support for Adblock-style filter lists and regular expressions. It can fetch rules from multiple URLs and supports custom rule files.
    **强大的广告拦截：** 内置广告拦截器，支持 Adblock 风格的过滤列表和正则表达式。它可以从多个 URL 获取规则，并支持自定义规则文件。

*   **Multi-layer Caching:**
    **多层缓存：**
    *   **Raw Cache:** Caches the initial, unsorted responses from upstream servers for a fast initial response.
        **原始缓存：** 缓存来自上游服务器的初始、未排序的响应，以实现快速的首次响应。
    *   **Sorted Cache:** Caches the final, ping-sorted list of IPs.
        **排序缓存：** 缓存经过 ping 排序后的最终 IP 列表。
    *   **Error Cache:** Caches `NXDOMAIN` responses to avoid repeated queries for non-existent domains.
        **错误缓存：** 缓存 `NXDOMAIN`（域名不存在）响应，以避免对不存在的域名进行重复查询。
    *   **Ad-block Cache:** Caches decisions for allowed and blocked domains to speed up filtering.
        **广告拦截缓存：** 缓存允许和拦截的域名决策，以加快过滤速度。

*   **Asynchronous Operations:** IP sorting and cache refreshing are performed in the background to avoid blocking DNS queries.
    **异步操作：** IP 排序和缓存刷新在后台执行，以避免阻塞 DNS 查询。

*   **Domain Prefetching:** Proactively refreshes the cache for frequently accessed domains before they expire.
    **域名预取：** 在热门域名缓存过期前主动刷新，确保访问速度。

*   **Web Interface:** A web UI to view statistics, recent queries, cache entries, and manage ad-block settings.
    **Web 界面：** 提供一个 Web UI 来查看统计信息、最近的查询、缓存条目以及管理广告拦截设置。

*   **Configuration Hot-Reload:** Most configuration changes can be applied without restarting the server.
    **配置热重载：** 大多数配置更改无需重启服务器即可应用。

*   **System Service:** Can be installed and managed as a `systemd` service on Linux.
    **系统服务：** 可以在 Linux 上作为 `systemd` 服务安装和管理。

*   **TCP/UDP Support:** Supports both UDP and TCP DNS queries.
    **TCP/UDP 支持：** 支持 UDP 和 TCP DNS 查询。

*   **IPv6 Ready:** Supports AAAA record queries and can return IPv6 addresses.
    **支持 IPv6：** 支持 AAAA 记录查询，并可以返回 IPv6 地址。

## How It Works / 工作原理

1.  A DNS query is received.
    收到一个 DNS 查询。
2.  **Ad-blocker:** The domain is first checked against the ad-blocking engine. If it's a blocked domain, a `NXDOMAIN` or `0.0.0.0` response is returned immediately.
    **广告拦截器：** 首先根据广告拦截引擎检查域名。如果域名被拦截，则立即返回 `NXDOMAIN` 或 `0.0.0.0` 响应。
3.  **Cache Check:** The server checks for a valid, sorted response in its cache. If found, the sorted IPs are returned instantly.
    **缓存检查：** 服务器在其缓存中检查是否存在有效的、已排序的响应。如果找到，则立即返回已排序的 IP。
4.  **Upstream Query:** If not cached, the query is sent to multiple upstream DNS servers simultaneously.
    **上游查询：** 如果缓存中没有，则同时将查询发送到多个上游 DNS 服务器。
5.  **Fast Response & Async Sort:** The server immediately returns an unsorted list of IPs to the client for a fast response, while simultaneously launching a background task to ping and sort the IPs.
    **快速响应与异步排序：** 服务器立即向客户端返回一个未排序的 IP 列表以实现快速响应，同时启动一个后台任务来对这些 IP 进行 ping 测试和排序。
6.  **Cache Update:** Once the sorting is complete, the sorted list of IPs is stored in the cache for subsequent requests.
    **缓存更新：** 排序完成后，排序好的 IP 列表将存储在缓存中，以备后续请求使用。

## Getting Started / 快速入门

### Prerequisites / 环境要求

*   Go 1.25 or later.
    Go 1.25 或更高版本。

### Build / 构建

You can build the `SmartDNSSort` executable using the provided build scripts or standard Go commands:
您可以使用提供的构建脚本或标准的 Go 命令来构建 `SmartDNSSort` 可执行文件：

```bash
# On Windows / 在 Windows 上
./build.bat

# On Linux/macOS / 在 Linux/macOS 上
./build.sh
```

Or manually:
或者手动构建：

```bash
go build -o SmartDNSSort ./cmd/main.go
```

### Configuration / 配置

SmartDNSSort is configured using a `config.yaml` file. A default configuration is provided. Key settings include:
SmartDNSSort 使用 `config.yaml` 文件进行配置。项目提供了一个默认配置文件。关键设置包括：

*   **`upstream.servers`**: A list of upstream DNS servers to query (e.g., `8.8.8.8`, `1.1.1.1`).
    **`upstream.servers`**: 用于查询的上游 DNS 服务器列表（例如 `8.8.8.8`, `1.1.1.1`）。
*   **`dns.listen_port`**: The port the DNS server listens on (default: `53`).
    **`dns.listen_port`**: DNS 服务器监听的端口（默认为 `53`）。
*   **`webui.enabled`**: Enable or disable the web interface.
    **`webui.enabled`**: 启用或禁用 Web 界面。
*   **`webui.listen_port`**: The port for the web interface (default: `8080`).
    **`webui.listen_port`**: Web 界面使用的端口（默认为 `8080`）。
*   **`adblock.enable`**: Enable or disable the ad-blocker.
    **`adblock.enable`**: 启用或禁用广告拦截器。
*   **`adblock.rule_urls`**: A list of URLs for ad-blocking filter lists.
    **`adblock.rule_urls`**: 用于广告拦截过滤列表的 URL 列表。
*   **`ping`**: Configuration for the IP optimization (ping utility).
    **`ping`**: IP 优选（ping 工具）的配置。
    *   **`ping.enabled`**: (Boolean) Enables or disables the IP optimization feature. When disabled, DNS query results are returned without ping testing and sorting, which can reduce CPU usage. Default: `true`.
        **`ping.enabled`**: (布尔值) 启用或禁用 IP 优选功能。禁用后，DNS 查询结果将不进行 ping 测试和排序，从而降低 CPU 使用。默认值：`true`。
*   **`cache`**: TTL settings and memory limits for the different cache layers.
    **`cache`**: 不同缓存层的 TTL 设置和内存限制。

### Running the Server / 运行服务器

To run the server, simply execute the binary and point it to your configuration file:
要运行服务器，只需执行二进制文件并指定您的配置文件：

```bash
./SmartDNSSort -c config.yaml
```

### Running as a Service (Linux) / 作为服务运行 (Linux)

On Linux systems with `systemd`, you can install, manage, and uninstall the service using the `-s` flag.
在具有 `systemd` 的 Linux 系统上，您可以使用 `-s` 标志来安装、管理和卸载服务。

```bash
# Install the service / 安装服务
sudo ./SmartDNSSort -s install -c /path/to/your/config.yaml

# Check the service status / 检查服务状态
sudo ./SmartDNSSort -s status

# Uninstall the service / 卸载服务
sudo ./SmartDNSSort -s uninstall
```

## Web Interface / Web 界面

If enabled, the web interface provides a simple way to monitor the server. By default, it's available at `http://localhost:8080`.
如果启用，Web 界面提供了一种简单的方法来监控服务器。默认情况下，可以通过 `http://localhost:8080` 访问。

The UI displays:
界面显示内容：
*   Real-time statistics (queries, cache hits, blocked domains).
    实时统计（查询、缓存命中、拦截的域名）。
*   A list of recent DNS queries.
    最近的 DNS 查询列表。
*   Ad-block status and rule sources.
    广告拦截状态和规则来源。
*   Cache inspection tools.
    缓存检查工具。

You can also use the web interface to dynamically change some configuration settings and hot-reload the server.
您还可以使用 Web 界面动态更改某些配置设置并热重载服务器。

## Command-line Arguments / 命令行参数

| Flag / 标志 | Description / 描述 | Default / 默认值 |
|---|---|---|
| `-c <path>` | Path to the configuration file. / 配置文件路径。 | `config.yaml` |
| `-s <command>` | System service management (Linux only): `install`, `uninstall`, `status`. / 系统服务管理（仅限 Linux）：`install`, `uninstall`, `status`。 | |
| `-w <path>` | Working directory for the service. / 服务的工作目录。 | Current directory / 当前目录 |
| `-user <name>` | User to run the service as (install only). / 运行服务的用户（仅限安装）。 | `root` |
| `-dry-run` | Preview service installation/uninstallation without making changes. / 预览服务的安装/卸载，而不实际执行更改。 | `false` |
| `-v` | Enable verbose output. / 启用详细输出。 | `false` |
| `-h` | Display help information. / 显示帮助信息。 | |