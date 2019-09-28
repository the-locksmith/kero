package entity

import (
	"github.com/galaco/kero/valve/entity"
)

// PropPhysicsOverride
type PropPhysicsOverride struct {
	entity.EntityBase
	PropBase
}

// Classname
func (entity PropPhysicsOverride) Classname() string {
	return "prop_physics_override"
}