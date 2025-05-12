package collections

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStack(t *testing.T) {
	t.Run("Basic Operations", func(t *testing.T) {
		assert := assert.New(t)
		s := NewStack[int](10)

		assert.Equal(0, s.Size(), "empty stack should have size 0")
		assert.True(s.IsEmpty(), "empty stack should be empty")

		s.Push(1)
		s.Push(2)

		assert.Equal(2, s.Size(), "stack should have size 2 after pushing 2 items")
		assert.False(s.IsEmpty(), "stack should not be empty after pushing items")

		val, ok := s.Pop()
		assert.True(ok, "pop should return true for non-empty stack")
		assert.Equal(2, val, "pop should return the last element pushed (LIFO)")

		assert.Equal(1, s.Size(), "stack should have size 1 after popping 1 item")

		val, ok = s.Pop()
		assert.True(ok, "pop should return true for non-empty stack")
		assert.Equal(1, val, "pop should return the first element pushed")

		val, ok = s.Pop()
		assert.False(ok, "pop should return false for empty stack")
		assert.Equal(0, val, "pop should return zero value for empty stack")
	})

	t.Run("Empty Stack", func(t *testing.T) {
		assert := assert.New(t)
		s := NewStack[int](10)

		val, ok := s.Pop()
		assert.False(ok, "pop should return false for empty stack")
		assert.Equal(0, val, "pop should return zero value for empty stack")

		val, ok = s.Peek()
		assert.False(ok, "peek should return false for empty stack")
		assert.Equal(0, val, "peek should return zero value for empty stack")
	})

	t.Run("Peek Method", func(t *testing.T) {
		assert := assert.New(t)
		s := NewStack[int](10)
		
		s.Push(1)
		s.Push(2)

		val, ok := s.Peek()
		assert.True(ok, "peek should return true for non-empty stack")
		assert.Equal(2, val, "peek should return the top element without removing it")
		assert.Equal(2, s.Size(), "size should not change after peek")

		// Verify pop still works correctly after peek
		val, ok = s.Pop()
		assert.True(ok)
		assert.Equal(2, val, "pop should return the top element after peek")
	})

	t.Run("Clear Method", func(t *testing.T) {
		assert := assert.New(t)
		s := NewStack[int](10)
		
		s.Push(1)
		s.Push(2)

		assert.Equal(2, s.Size(), "stack should have size 2 before clear")

		s.Clear()

		assert.Equal(0, s.Size(), "stack should be empty after clear")
		assert.True(s.IsEmpty(), "stack should be empty after clear")

		// Verify the stack is usable after clear
		s.Push(3)
		assert.Equal(1, s.Size(), "stack should have size 1 after pushing to cleared stack")
		
		val, ok := s.Pop()
		assert.True(ok)
		assert.Equal(3, val, "element should be retrievable after clear")
	})

	t.Run("Range Method", func(t *testing.T) {
		assert := assert.New(t)
		s := NewStack[int](10)
		
		// Push elements
		s.Push(1)
		s.Push(2)
		s.Push(3)

		// Collect elements using Range
		var elements []int
		s.Range(func(v int) {
			elements = append(elements, v)
		})

		// Elements should be in top-to-bottom order (LIFO)
		expected := []int{3, 2, 1}
		assert.Equal(expected, elements, "range should iterate from top to bottom")
		
		// Verify stack remains unchanged after Range
		assert.Equal(3, s.Size(), "size should not change after Range")
		
		// Verify the elements can still be popped in correct order
		for i := 3; i >= 1; i-- {
			val, ok := s.Pop()
			assert.True(ok)
			assert.Equal(i, val, "elements should be popped in LIFO order after Range")
		}
	})

	t.Run("Capacity Expansion", func(t *testing.T) {
		assert := assert.New(t)
		initialCapacity := uint32(5)
		s := NewStack[int](initialCapacity)
		
		// Push more elements than initial capacity
		for i := 1; i <= 10; i++ {
			s.Push(i)
		}
		
		assert.Equal(10, s.Size(), "stack should correctly hold more elements than initial capacity")
		
		// Verify LIFO order is maintained after expansion
		for i := 10; i >= 1; i-- {
			val, ok := s.Pop()
			assert.True(ok)
			assert.Equal(i, val, "LIFO order should be maintained after capacity expansion")
		}
	})

	t.Run("Thread Safety", func(t *testing.T) {
		// This is a basic test for concurrent operations
		// In a real scenario, you might want to use more sophisticated
		// race detection or stress testing
		s := NewStack[int](10)
		
		// Just verify that we can perform operations without panic
		// True race condition testing would require a more complex setup
		s.Push(1)
		s.Size()
		s.IsEmpty()
		s.Peek()
		s.Pop()
	})

	t.Run("Different Data Types", func(t *testing.T) {
		assert := assert.New(t)
		
		// Test with string type
		sString := NewStack[string](10)
		sString.Push("hello")
		sString.Push("world")
		
		val, ok := sString.Pop()
		assert.True(ok)
		assert.Equal("world", val)
		
		// Test with custom struct type
		type TestStruct struct {
			ID   int
			Name string
		}
		
		sStruct := NewStack[TestStruct](10)
		sStruct.Push(TestStruct{ID: 1, Name: "item1"})
		sStruct.Push(TestStruct{ID: 2, Name: "item2"})
		
		structVal, ok := sStruct.Pop()
		assert.True(ok)
		assert.Equal(2, structVal.ID)
		assert.Equal("item2", structVal.Name)
	})
}
