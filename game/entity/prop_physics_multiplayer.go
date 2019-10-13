package entity

import (
	"github.com/galaco/kero/framework/entity"
)

// PropPhysicsMultiplayer
type PropPhysicsMultiplayer struct {
	entity.EntityBase
	PropRenderableBase
}

// Classname
func (entity PropPhysicsMultiplayer) Classname() string {
	return "prop_physics_multiplayer"
}