// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.
// ------------------------------------------------------------

package custom

import (
	"fmt"

	grpc_go "google.golang.org/grpc"
)

type (
	//Metadata for custom component
	Metadata struct {
		Properties map[string]string `json:"properties"`
		Name       string            `json:"name"`
	}

	// CustomComponent to register a custom gRPC endpoint
	CustomComponent interface {
		Init(metadata Metadata) error
		RegisterServer(s *grpc_go.Server) error
	}

	//Custom encapsulates a factory of custom components
	Custom struct {
		Name          string
		FactoryMethod func() CustomComponent
	}

	// Registry is an interface for a component that returns registered custom implementations
	Registry interface {
		Register(components ...Custom)
		CreateCustomComponent(name string) (CustomComponent, error)
	}

	//customRegistry implementation of Registry
	customRegistry struct {
		customComponents map[string]func() CustomComponent
	}
)

//New creates a custom components factory
func New(name string, factoryMethod func() CustomComponent) Custom {
	return Custom{
		Name:          name,
		FactoryMethod: factoryMethod,
	}
}

// NewRegistry is used to create state store registry.
func NewRegistry() Registry {
	return &customRegistry{
		customComponents: map[string]func() CustomComponent{},
	}
}

//Register registers a new factory method that creates an instance of a Custom.
//The key is the name of the custom component, eg. custom.somefunc
func (s *customRegistry) Register(components ...Custom) {
	for _, component := range components {
		s.customComponents[createFullName(component.Name)] = component.FactoryMethod
	}
}

// Create instantiates an custom component based on `name`.
func (s *customRegistry) CreateCustomComponent(name string) (CustomComponent, error) {
	if method, ok := s.customComponents[name]; ok {
		return method(), nil
	}
	return nil, fmt.Errorf("couldn't find custom component %s", name)
}

func createFullName(name string) string {
	return fmt.Sprintf("custom.%s", name)
}
