package vanilla

import "testing"

func TestFlavorCanBeCreated(t *testing.T) {
	flavor := &Flavor{}
	if flavor == nil {
		t.Fatalf("Flavor must be created")
	}
}
