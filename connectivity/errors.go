package connectivity

import "errors"

// ErrNetworkOffline 网络离线错误
// 用于 Fast Fail 机制，当网络不可用时直接返回此错误
var ErrNetworkOffline = errors.New("network offline")
