package inventory

import (
	"encoding/json"
	"sync"
)

// Items ...
type Items map[string]Item

// Item ...
type Item map[string]interface{}

// Inventory is the data type for inventory data produced by an integration data
// source and emitted to the agent's inventory data store.
type Inventory struct {
	items Items
	lock  sync.Mutex
}

// MarshalJSON Marshals the items map into a JSON
func (i Inventory) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.items)
}

// SetItem stores a value into the inventory data structure.
// key should be at most 512 character length.
func (i Inventory) SetItem(key string, field string, value interface{}) {
	i.lock.Lock()
	defer i.lock.Unlock()

	if _, ok := i.items[key]; ok {
		i.items[key][field] = value
	} else {
		i.items[key] = Item{field: value}
	}
}

// Item returns stored item
func (i Inventory) Item(key string) (item Item, exists bool) {
	item, exists = i.items[key]
	return
}

// Items returns all stored items
func (i Inventory) Items() Items {
	return i.items
}

// New creates new inventory.
func New() *Inventory {
	return &Inventory{
		items: make(Items),
	}
}