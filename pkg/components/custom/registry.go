// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.
// ------------------------------------------------------------

package custom

import (
	"fmt"

	grpc_go "google.golang.org/grpc"
)

//Metadata
type Metadata struct {
	Properties map[string]string `json:"properties"`
	Name       string            `json:"name"`
}

// CustomComponent component
type CustomComponent interface {
	Init(metadata Metadata) error
	RegisterServer(s *grpc_go.Server) error
}

type Custom struct {
	Name          string
	FactoryMethod func() CustomComponent
}

func New(name string, factoryMethod func() CustomComponent) Custom {
	return Custom{
		Name:          name,
		FactoryMethod: factoryMethod,
	}
}

// Registry is an interface for a component that returns registered state store implementations
type Registry interface {
	Register(components ...Custom)
	CreateCustomComponent(name string) (CustomComponent, error)
}

type customRegistry struct {
	customMap map[string]func() CustomComponent
}

// NewRegistry is used to create state store registry.
func NewRegistry() Registry {
	return &customRegistry{
		customMap: map[string]func() CustomComponent{},
	}
}

// // Register registers a new factory method that creates an instance of a StateStore.
// // The key is the name of the state store, eg. redis.
func (s *customRegistry) Register(components ...Custom) {
	for _, component := range components {
		s.customMap[createFullName(component.Name)] = component.FactoryMethod
	}
}

func (s *customRegistry) CreateCustomComponent(name string) (CustomComponent, error) {
	if method, ok := s.customMap[name]; ok {
		return method(), nil
	}
	return nil, fmt.Errorf("couldn't find state store %s", name)
}

func createFullName(name string) string {
	return fmt.Sprintf("state.%s", name)
}
