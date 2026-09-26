package gd

import "strings"

type Handler func(*Ctx) error

type route struct {
	parts   []string
	handler Handler
}

type Router struct {
	routes map[string][]route
}

func NewRouter() *Router {
	return &Router{routes: make(map[string][]route)}
}

func (rt *Router) Add(method, path string, h Handler) {
	rt.routes[method] = append(rt.routes[method], route{parts: splitPath(path), handler: h})
}

func (rt *Router) Match(method, path string) (Handler, map[string]string, bool) {
	parts := splitPath(path)
	for _, r := range rt.routes[method] {
		if params, ok := matchParts(r.parts, parts); ok {
			return r.handler, params, true
		}
	}
	return nil, nil, false
}

func (rt *Router) HasPathButOtherMethod(method, path string) bool {
	parts := splitPath(path)
	for m, routes := range rt.routes {
		if m == method {
			continue
		}
		for _, r := range routes {
			if _, ok := matchParts(r.parts, parts); ok {
				return true
			}
		}
	}
	return false
}

func splitPath(p string) []string {
	if p == "" || p == "/" {
		return nil
	}
	return strings.Split(strings.Trim(p, "/"), "/")
}

func matchParts(pattern, actual []string) (map[string]string, bool) {
	params := make(map[string]string)
	for i, p := range pattern {
		if i >= len(actual) {
			return nil, false
		}
		switch {
		case strings.HasPrefix(p, ":"):
			params[p[1:]] = actual[i]
		case p == "*":
			params["*"] = strings.Join(actual[i:], "/")
			return params, true
		case p != actual[i]:
			return nil, false
		}
	}
	if len(actual) != len(pattern) {
		return nil, false
	}
	return params, true
}
