package ecs

import (
	"sort"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// tableID identifies a specific archetype table.
type tableID int

const noTable tableID = -1

// archetypeKey is a sorted slice of ComponentIDs that uniquely identifies
// an archetype (the set of component types an entity holds).
type archetypeKey []ComponentID

func (k archetypeKey) equals(other archetypeKey) bool {
	if len(k) != len(other) {
		return false
	}
	for i := range k {
		if k[i] != other[i] {
			return false
		}
	}
	return true
}

// table stores entity membership for one archetype. Component data lives
// in the component pool; tables only track which entities belong here.
type table struct {
	key      archetypeKey
	entities []Entity
	count    int
}

func newTable(key archetypeKey, l *logx.Logger) *table {
	l.Debug("table created", "archetype", key)
	return &table{
		key:      key,
		entities: make([]Entity, 0, 64),
	}
}

// add inserts an entity. Returns the row index.
func (t *table) add(e Entity) int {
	row := t.count
	t.entities = append(t.entities, e)
	t.count++
	return row
}

// remove removes an entity at the given row. Uses swap-with-last for O(1).
// Returns the removed entity and the entity that was swapped into its position.
// swapped == removed means no swap occurred (the removed entity was last).
func (t *table) remove(row int, l *logx.Logger) (removed Entity, swapped Entity) {
	last := t.count - 1
	removed = t.entities[row]
	swapped = removed // sentinel: no swap
	if row != last {
		t.entities[row] = t.entities[last]
		swapped = t.entities[row]
	}
	t.entities = t.entities[:last]
	t.count--

	l.Debug("entity removed from table", "entity", removed, "archetype", t.key, "row", row)
	return removed, swapped
}

func (t *table) len() int {
	return t.count
}

// tableGraph maps archetype keys to table IDs and provides transition lookup.
type tableGraph struct {
	tables []*table
	index  map[string]tableID
}

func keyString(key archetypeKey) string {
	b := make([]byte, len(key)*4)
	for i, id := range key {
		b[i*4] = byte(id)
		b[i*4+1] = byte(id >> 8)
		b[i*4+2] = byte(id >> 16)
		b[i*4+3] = byte(id >> 24)
	}
	return string(b)
}

func newTableGraph(l *logx.Logger) *tableGraph {
	l.Debug("table graph created")
	return &tableGraph{
		tables: make([]*table, 0, 16),
		index:  make(map[string]tableID),
	}
}

func (g *tableGraph) getOrCreate(key archetypeKey, l *logx.Logger) tableID {
	ks := keyString(key)
	if id, ok := g.index[ks]; ok {
		return id
	}
	id := tableID(len(g.tables))
	t := newTable(key, l)
	g.tables = append(g.tables, t)
	g.index[ks] = id
	l.Debug("table registered", "table_id", id, "archetype", key)
	return id
}

func (g *tableGraph) get(id tableID) *table {
	if id < 0 || int(id) >= len(g.tables) {
		return nil
	}
	return g.tables[int(id)]
}

func (g *tableGraph) transitionAdd(current tableID, addCID ComponentID, l *logx.Logger) tableID {
	currentTable := g.get(current)
	var key archetypeKey
	if currentTable != nil {
		key = make(archetypeKey, len(currentTable.key), len(currentTable.key)+1)
		copy(key, currentTable.key)
	}
	pos := sort.Search(len(key), func(i int) bool { return key[i] >= addCID })
	if pos < len(key) && key[pos] == addCID {
		return current
	}
	key = append(key, 0)
	copy(key[pos+1:], key[pos:])
	key[pos] = addCID
	return g.getOrCreate(key, l)
}

func (g *tableGraph) transitionRemove(current tableID, removeCID ComponentID, l *logx.Logger) tableID {
	currentTable := g.get(current)
	if currentTable == nil {
		return noTable
	}
	var key archetypeKey
	for _, cid := range currentTable.key {
		if cid != removeCID {
			key = append(key, cid)
		}
	}
	if len(key) == 0 {
		return g.getOrCreate(archetypeKey{}, l)
	}
	return g.getOrCreate(key, l)
}
