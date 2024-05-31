package migrations

import "fmt"

// registryData copies data from the disk to the seaweedfs s3 store.
// if it fails, it will scale the registry deployment back to 1.
// if it succeeds, it will create a secret used to indicate success to the operator.
func registryData() error {
	// TODO
	fmt.Printf("registryData\n")
	return nil
}

// registryScale scales the registry deployment to the given replica count.
// '0' and '1' are the only acceptable values.
func registryScale(scale int) error {
	// TODO
	fmt.Printf("registryScale %d\n", scale)
	return nil
}
