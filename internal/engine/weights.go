package engine

// markWeightsBound records that the current process has loaded a .cact
// archive into the engine of the given generation. Called by the worker
// child after a successful needle_load.
func markWeightsBound(gen int) error {
	stateMu.Lock()
	defer stateMu.Unlock()
	weightsBound[gen] = true
	return nil
}
