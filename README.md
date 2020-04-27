# offloader

[![Actions Status](https://github.com/wistia/offloader/workflows/CI/badge.svg)](https://github.com/wistia/offloader/actions)

offloader is a lightweight proxy for offloading long-running requests from backends that have limited
parallelism. Its behavior is configurable via response headers from the backend. It's designed to be used with Go's 
[httputil.ReverseProxy](https://golang.org/pkg/net/http/httputil/#ReverseProxy) as a  `ModifyResponse` function.

For example, you may have a Ruby on Rails backend server that can handle a limited number of parallel requests, and
the backend server occasionally needs to make long-running requests to another internal service. With offloader sitting
in front of your backend server, the backend can defer the long-running work to offloader, freeing up the Rails web
workers to handle another client request. In the meantime, offloader handles the long-running request and responds to
the original client when the response is ready. Go's excellent support for parallelism means that you can offload
a significant number of requests.

```
  client <--> offloader <--> backend server
                  \
                    <--> offload server 
```

offloader is inspired by NGINX's [X-Accel-Redirect](https://www.nginx.com/resources/wiki/start/topics/examples/x-accel/#x-accel-redirect)
feature and aims to solve a more general version of the same problem. 

## Getting Started

```golang
import "github.com/wistia/offloader"

...

proxy := httputil.NewSingleHostReverseProxy(...)
proxy.ModifyResponse = offloader.Handler
```

In this example, `proxy` is an `http.Handler` and is typically used with an `http.Server` instance.

## Controlling offload behavior

You can control offloader's behavior using response headers from your backend server.

| Header                       | Possible values       | Behavior |
| ---                          | ---                   | --- |
| Offload-Requested            | (Any)                 | If this header is present, then offload is enabled. Otherwise, the backend response is passed directly back to the client. |
| Offload-Url                  | (Any valid URL)       | The protocol, host, and path for the offload request are determined from this URL. |
| Offload-Method               | "GET", "POST", "HEAD" | The request to the offload server uses this HTTP method. |
| Offload-Forward-Body         | (Any)                 | If this header is present, then the response body from the backend will be sent as the _request_ body to the offload server. This is useful for making POST requests to the offload server. |
| Offload-X-<your-header-name> | (Any)                 | Headers in this format will be passed to the offload server as `Your-Header-Name`. |
