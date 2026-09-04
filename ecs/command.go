package ecs

import (
	"unsafe"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// cmdKind enumerates the types of deferred commands.
type cmdKind uint8

const (
	cmdCreate cmdKind = iota
	cmdDestroy
	cmdAdd
	cmdRemove
	cmdAttach
	cmdDetach
)

// command is a single deferred structural operation.
type command struct {
	kind        cmdKind
	entity      Entity
	componentID ComponentID
	data        unsafe.Pointer
	handle      Handle
	cascade     bool
}

// commandBuffer collects structural operations and applies them on Flush.
type commandBuffer struct {
	commands []command
}

func newCommandBuffer(l *logx.Logger) *commandBuffer {
	l.Debug("command buffer created")
	return &commandBuffer{
		commands: make([]command, 0, 64),
	}
}

func (b *commandBuffer) push(cmd command) {
	b.commands = append(b.commands, cmd)
}

func (b *commandBuffer) len() int {
	return len(b.commands)
}

func (b *commandBuffer) clear() {
	b.commands = b.commands[:0]
}

// apply executes all buffered commands against the world.
func (b *commandBuffer) apply(w *World) {
	if len(b.commands) == 0 {
		return
	}

	w.log.Debug("flushing command buffer", "commands", len(b.commands))

	for _, cmd := range b.commands {
		switch cmd.kind {
		case cmdCreate:
			w.entities.create(w.log)
		case cmdDestroy:
			w.destroyEntity(cmd.entity, cmd.cascade)
		case cmdAdd:
			info := w.registry.componentInfoFor(cmd.componentID)
			if info != nil {
				w.addComponent(cmd.entity, cmd.componentID, cmd.data, info.size)
			}
		case cmdRemove:
			w.removeComponent(cmd.entity, cmd.componentID)
		case cmdAttach:
			w.attachHandle(cmd.entity, cmd.componentID, cmd.handle)
		case cmdDetach:
			w.detachComponent(cmd.entity, cmd.componentID)
		}
	}

	w.log.Debug("flush complete", "applied", len(b.commands))
	b.clear()
}
