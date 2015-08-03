// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/


/*

	Mnemonic:	mlag
	Abstract:	Manages a list of links that belong to the same mlag group. The mlags track
				links using a link's obligation so that if multiple links share the same
				obligation (bidirectional), then we avoid having to worry about referencing
				the same obligation twice.

	Date:		28 Jul 2014
	Author:		E. Scott Daniels

	Mods:		

*/

package gizmos

import (
	//"bufio"
	//"fmt"
	//"os"
	//"strings"
	//"time"
)

// --------------------------------------------------------------------------------------

/*
	Defines an mlag which is a name and a list of link pointers.
*/
type Mlag struct {
	name	*string
	llist	[]*Obligation	// list of links (tracked by obligation so as not to dup links that share the obligation)
	lidx	int			// last non-nil value in the list
}

/*
	Create an mlag struct and return a pointer to it. Nil pointer
	indicates error.
*/
func Mk_mlag( name *string, lob *Obligation ) ( m *Mlag ) {
	m = nil
	if name == nil {			// not permitted
		return
	}

	m = &Mlag {
		name: name,
	}
	
	m.llist = make( []*Obligation, 10 )
	if lob != nil {
		m.llist[0] = lob
		m.lidx = 1
	}

	return
}

/*
	Add a link to the mlag set.
*/
func (m *Mlag) Add_link( lob *Obligation ) {
	
	if m == nil || lob == nil {
		return
	}

	nil_entry := m.lidx						// insert into a hole if found
	for i := 0; i < m.lidx; i++ {			// we prevent dups with a search; may want to hash on link name in future
		if m.llist[i] == nil {
			nil_entry = i
		} else {		
			if m.llist[i] == lob {			// already referenced
				return
			}
		}
	}

	if nil_entry >= len( m.llist ) {
		new_list := make( []*Obligation, m.lidx + 10 )
		for i := range m.llist {
			new_list[i] = m.llist[i]		// copy into new
		}

		m.llist = new_list
	}

	m.llist[nil_entry] = lob
	if nil_entry >= m.lidx {				// bump  only if we added to end rather than replaced nil entry
		m.lidx++	
	}
}


/*
	Remove a link from the mlag group.
*/
func (m *Mlag) Rm_link( lob *Obligation ) {
	for i := 0; i < m.lidx; i++ {
		if m.llist[i] == lob {
			m.llist[i] = nil
			if i == m.lidx - 1 {				// dec end marker if last in list deleted
				m.lidx--
			}
			return
		}
	}
}

/*
	Run each link in the list and increase the utilisation of the link. We will _not_ inc the utilisation
	of the obligation that is passed in assuming it was bumpped initially which triggered the inc across
	the mlag.
*/
func (m *Mlag) Inc_utilisation( commence int64, conclude int64, delta int64, usr *Fence, skip *Obligation ) {
	for i := 0; i < m.lidx; i++ {
		if m.llist[i] != nil && m.llist[i] != skip {
			msg := m.llist[i].Inc_utilisation( commence, conclude, delta, usr )	
			if msg != nil {
				obj_sheep.Baa( 1, "utilisation increased for mlag %s: %s", m.name,  *msg )
			}
		}
	}
}
