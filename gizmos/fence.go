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
	Name	*string		// associated ID/name; available from outside for convenience
	max_cap	int64		// max amount allowed to be contained
	min_cap	int64		// min amount allowed to be contained
	value	int64		// current capacity
}

/*
	Creates a fence with given max/min capacities and an intial value.
*/
func Mk_fence( name *string, max int64, min int64, init_val int64 ) ( f *Fence ) {

	f = &Fence { 
		Name:		name,
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
func (f *Fence ) Inc_used( c int64 ) {
	f.value += c
	if f.value > f.max_cap {
		obj_sheep.Baa( 1, "WRN: clipping fence cap: max=%d cvalue=%d inc=%d tot=%d", f.max_cap, f.value, c, c+f.value )	 // suggests something is wrong since it should not reach here if over
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
func (f *Fence ) Inc_if_room( c int64 ) ( bool ) {
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
	f.Inc_used( c )

	return f.value
}

/*
	Accepts a requested additional capacity and returns the total capacity allowed (have)
	by the fence and the value that is needed (current allocation + additional amount).
*/
func (f *Fence) Get_have_need( addtl_cap int64 ) ( have int64, need int64 ) {
	if addtl_cap < 0 {
		return f.min_cap, f.value + addtl_cap	
	} 

	return f.max_cap, f.value + addtl_cap
}

/*
	Returns the max limit.
*/
func (f *Fence ) Get_limit_max( ) ( int64 ) {
	return f.max_cap
}

/*
	Returns the min limit.
*/
func (f *Fence ) Get_limit_min( ) ( int64 ) {
	return f.min_cap
}

/*
	Returns the max and min capacity limits.
*/
func (f *Fence ) Get_limits( ) ( max int64, min int64 ) {
	return f.max_cap, f.min_cap
}

/*
	Create a copy of the fence. If a capacity value is passed in the function 
	will check the current min/max and if less than 100 assume they are a 
	percentage and compute the actual min/max using the percentage of the 
	capacity. If capacity is 0, then no check is made.
*/
func (f *Fence) Clone( capacity int64 ) ( *Fence ) {
	nf := Mk_fence( f.Name, f.max_cap, f.min_cap, f.value )
	if capacity > 0 {
		if nf.max_cap < 101 {
			nf.max_cap = (capacity/100) * nf.max_cap		// assume max_cap is a percentage, so adjust
		}	
		if nf.min_cap < 101 {
			nf.min_cap = (capacity/100) * nf.min_cap		// assume max_cap is a percentage, so adjust
		}	
	}

	return nf
}

/*
	Creates a new object and copies the information replacing the name in the new 
	object with the name passed in. 
*/
func (f *Fence) Copy( new_name *string ) ( *Fence ) {
	return Mk_fence( new_name, f.max_cap, f.min_cap, f.value )
}


/*
	Jsonise the whole object.
*/
func (f *Fence) To_json( ) ( s string ) {

	if f == nil {
		s = ``
		return
	}

	s = fmt.Sprintf( `{ "name": %q, "max": %d, "min": %d, "value": %d }`, *f.Name, f.max_cap, f.min_cap, f.value )

	return
}
