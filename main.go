package turbomesh

func main() {
	// `turbomesh server` 启动一个 DNS 服务器，用来把一个第一个就是 sub domain 转换成 IP 地址，格式是把 IPV4 的 XYZ换成.
	// 例如 192x168y0z1.example.com 就会被转换成 192.168.0.1

	// `turbomesh [PORT]` 启动一个 WebRTC 服务器，监听指定的端口，这个服务器会处理来自前端的 WebRTC 连接请求，转发到对应的本地HTTP服务。

}
