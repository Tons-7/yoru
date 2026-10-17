package storage_test

import (
	"testing"

	"yoru/internal/bt/storage"
	"yoru/internal/bt/test"
)

func TestBoltLeecherStorage(t *testing.T) {
	test.TestLeecherStorage(t, test.LeecherStorageTestCase{Name: "Boltdb", Factory: storage.NewBoltDB})
}
