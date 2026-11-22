//go:build !unix

package sync

// pidAlive cannot check processes portably here; stale locks are then
// only detected by age.
func pidAlive(int) bool { return true }
