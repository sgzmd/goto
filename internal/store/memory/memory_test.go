package memory

import (
	"testing"

	"goto/internal/link"
	"goto/internal/store/storetest"
)

func TestMemoryStoreContract(t *testing.T) {
	storetest.RunStoreContractTests(t, func(t *testing.T) link.Store {
		return New()
	})
}
