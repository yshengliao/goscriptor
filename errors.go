package goscriptor

import "errors"

var (
	// ErrNilClient is returned when a provided Redis client is nil.
	ErrNilClient = errors.New("goscriptor: client cannot be nil")
	
	// ErrNilOption is returned when a provided Option is nil.
	ErrNilOption = errors.New("goscriptor: option cannot be nil")
	
	// ErrScriptNotFound is returned when a script is not found in the local registry or Redis.
	ErrScriptNotFound = errors.New("goscriptor: script not found")
	
	// ErrKeyNotFound is returned when the script definition key does not exist in Redis.
	ErrKeyNotFound = errors.New("goscriptor: script key does not exist")
	
	// ErrScriptNotCached is returned when a script's SHA1 is in the registry but the script itself is not loaded in the Redis script cache.
	ErrScriptNotCached = errors.New("goscriptor: script not in cache, reload required")
)
