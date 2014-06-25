// vi: sw=4 ts=4:

/*

	Mnemonic:	fence
	Abstract:	Manages a current capacity and max/min values to keep something
				within limits. 
	Date:		25 June 2014
	Author:		E. Scott Daniels

*/

package gizmos

import (
	"fmt"
)

// --------------------------------------------------------------------------------------
/*
	defines a host
*/
type Fence struct {
	max_cap	int64		// max amount allowed to be contained
	min_cap	int64		// min amount allowed to be contained
	value	int64		// current capacity
}

/*
	Creates a fence with given max/min capacities and an intial value.
*/
func Mk_fence( max int64, min int64, init_val int64 ) ( f *Fence ) {

	f = &Fence { 
		max_cap: 	max,
		min_cap:	min,
		value:		init_val,	
	}

	return
}

/*
	Tests current setting to see if adding c takes the value beyond either limit (c
	may be negative). Returns true if c can be added to the current value without
	busting the limit.
*/
func (f *Fence ) Has_capacity( c int64 ) ( bool ) {
	if c + f.value <= f.max_cap  && c + f.value >= f.min_cap {
		return true
	}

	return false
}

/*
	Blindly adds the capacity c to the current value and clips if the 
	new value exceeds a limit. 
*/
func (f *Fence ) Add_capacity( c int64 ) {
	f.value += c
	if f.value > f.max_cap {
		f.value = f.max_cap
	} else {
		if f.value <= f.min_cap {
			f.value = f.min_cap
		}
	}
}

/*
	Checks to see if capacity can be added to the current value without 
	violating a capacity limit. If it can be, then the value is added, else
	it is not and false is returned.
*/
func (f *Fence ) Add_if_room( c int64 ) ( bool ) {
	if f.Has_capacity( c ) {
		f.value += c 
		return true
	}

	return false
}

/*
	Returns the current value. 
*/
func (f *Fence ) Get_value() ( int64 ) {
	return f.value
}

/*
	Sets the value to c and clips if it's beyond a limit. 
	The actual value set is returned.
*/
func (f *Fence ) Set_value( c int64 ) ( int64 ) {
	f.value = 0
	f.Add_capacity( c )

	return f.value
}

/*
	Returns the max and min capacity limits.
*/
func (f *Fence ) Get_limits( ) ( max int64, min int64 ) {
	return f.max_cap, f.min_cap
}

/*
	Create a copy of the fence
*/
func (f *Fence) Clone( ) ( *Fence ) {
	return Mk_fence( f.max_cap, f.min_cap, f.value )
}


/*
	jsonise the whole object
*/
func (f *Fence) To_json( ) ( s string ) {

	if f == nil {
		s = ``
		return
	}

	s = fmt.Sprintf( `{ "max": %d, "min": %d, "value": %d }`, f.max_cap, f.min_cap, f.value )

	return
}
