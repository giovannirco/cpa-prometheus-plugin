//go:build cgo

package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/giovannirco/cpa-prometheus-plugin/internal/plugin"
)

var (
	runtimeMu sync.Mutex
	runtime   *plugin.Runtime
)

func currentRuntime() *plugin.Runtime {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if runtime == nil {
		runtime = plugin.NewRuntime(plugin.NewCallbackHost(hostCall))
	}
	return runtime
}

func hostCall(method string, request []byte) ([]byte, error) {
	n, err := boundedLen(uint64(len(request)), maxHostRequestBytes)
	if err != nil {
		return nil, err
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var response C.cliproxy_buffer
	var req *C.uint8_t
	if n > 0 {
		req = (*C.uint8_t)(C.CBytes(request[:n]))
		defer C.free(unsafe.Pointer(req))
	}
	code := C.call_host_api(cMethod, req, C.size_t(n), &response)
	if response.ptr != nil {
		defer C.free_host_buffer(response.ptr, response.len)
	}
	if code != 0 {
		return nil, fmt.Errorf("host callback %s returned %d", method, int(code))
	}
	outLen, err := boundedLen(uint64(response.len), maxHostResponseBytes)
	if err != nil {
		return nil, err
	}
	if response.ptr == nil || outLen == 0 {
		return nil, fmt.Errorf("host callback %s returned no response", method)
	}
	return C.GoBytes(response.ptr, C.int(outLen)), nil
}

func boundedGoString(p *C.char, max int) (string, error) {
	if p == nil {
		return "", fmt.Errorf("method is required")
	}
	n := 0
	base := unsafe.Pointer(p)
	for n < max {
		if *(*byte)(unsafe.Add(base, n)) == 0 {
			if n == 0 {
				return "", fmt.Errorf("method is required")
			}
			return C.GoStringN(p, C.int(n)), nil
		}
		n++
	}
	return "", fmt.Errorf("method exceeds %d bytes", max)
}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, pluginAPI *C.cliproxy_plugin_api) C.int {
	if pluginAPI == nil {
		return 1
	}
	if host == nil || host.abi_version != C.uint32_t(pluginABIVersion) {
		return 1
	}
	runtimeMu.Lock()
	C.store_host_api(host)
	runtime = plugin.NewRuntime(plugin.NewCallbackHost(hostCall))
	runtimeMu.Unlock()
	pluginAPI.abi_version = C.uint32_t(pluginABIVersion)
	pluginAPI.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	pluginAPI.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	pluginAPI.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	name, err := boundedGoString(method, maxHostMethodBytes)
	if err != nil {
		writeResponse(response, []byte(`{"ok":false,"error":{"code":"invalid_method","message":"method is required or too long"}}`))
		return 1
	}
	n, err := boundedLen(uint64(requestLen), maxHostRequestBytes)
	if err != nil {
		writeResponse(response, []byte(`{"ok":false,"error":{"code":"invalid_request","message":"request exceeds plugin limit"}}`))
		return 1
	}
	var req []byte
	if request != nil && n > 0 {
		req = C.GoBytes(unsafe.Pointer(request), C.int(n))
	}
	writeResponse(response, currentRuntime().Handle(name, req))
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, length C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = length
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	runtimeMu.Lock()
	rt := runtime
	runtime = nil
	runtimeMu.Unlock()
	if rt != nil {
		_ = rt.Handle("plugin.shutdown", nil)
	}
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	n, err := boundedLen(uint64(len(raw)), maxHostResponseBytes)
	if err != nil {
		return
	}
	ptr := C.CBytes(raw[:n])
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(n)
}
