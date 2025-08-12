package lock

func (locker *EtcdLocker) Wait(key string) error {
	lock, err := locker.WaitAcquire(key, 1)
	if err != nil {
		return err
	}
	err = lock.Release()
	if err != nil {
		return err
	}
	return nil
}
