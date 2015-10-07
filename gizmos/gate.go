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

	Mnemonic:	gate
	Abstract:	Manages a  gate which supports an ingress gate where we can
				potentially supply rate limiting in the outbound direction
				from src-host to dest-host.

	Date:		09 June 2015
	Author:		E. Scott Daniels

	Mod:		17 Jun 2015 - Added inc_utilisation() function and support
				to modifify underlying queues in the links.
*/

package gizmos

import (
	"fmt"
	"strings"
)

type Gate struct {
	gsw 	*Switch			// the switch on which the gate is applied (to which src is attached)
	src		*Endpt			// source host for flow-mods etc.
	dest	*Endpt			// destination host for flow-mods etc. (nil if dest is cross project/external
	ext_ip	*string			// the external IP address (we won't have dest host if external)
	bandw	int64			// amount of bandwidth to be used by the gate (if rate limiting)
	usr		*string			// the project (a.k.a. user) associated so we can manage fences at delete time
}

// ---------------------------------------------------------------------------------------

/*
	Creates a gate structure representing gated traffic from the source
	host to the dest host.  The switch is the switch that the source
	is attached to and where any gating flow-mods are going to be set.
*/
func Mk_gate( src *Endpt, dest *Endpt, gsw *Switch, bandw int64, usr string ) ( g *Gate ) {
	g = &Gate {
		src:		src,
		dest:		dest,
		gsw:		gsw,
		bandw:		bandw,
	}

	g.usr = &usr

	return
}

/*
	Set the amount of bandwith that has been reserved along this path.
*/
func (g *Gate) Set_bandwidth( bw int64 ) {
	if( bw > 0 ) {
		g.bandw = bw;
	}

	return
}

/*
	Allow the user to be associated.
*/
func (g *Gate) Set_usr( usr string ) {
	if g != nil {
		g.usr = &usr
	}
}

/*
	If there is no dest, this associates an IP address to use as the external destination
	IP address (needed for flow-mods and the like).
*/
func (g *Gate) Set_extip( ip *string ) {
	if g == nil {
		return
	}

	dup_str := ""
	if strings.Index( *ip, "/" ) < 0 {
		dup_str = *ip
	} else {
		tokens := strings.Split( *ip, "/" ) 		// assume !/IP or maybe !//IP
		dup_str = tokens[len(tokens)-1]
	}

	g.ext_ip = &dup_str
}

/*
	Returns the current user (project) associated with the gate.
*/
func (g *Gate) Get_usr( ) ( *string ) {
	if g != nil {
		return  g.usr
	}

	return nil
}

/*
	Returns the external IP address that was saved with the gate.
*/
func (g *Gate) Get_extip( ) ( *string ) {
	if g != nil {
		return  g.ext_ip
	}

	return nil
}

/*
	Returns true if the destination referenced by the gate is an external IP address
	(implying that it cannot be converted to a MAC address for flow-mods).
*/
func (g *Gate) Dest_is_ext( ) ( bool ) {
	if g != nil {
		return  g.ext_ip != nil		// if external ip assume external/;w
	}

	return true						// if g is nil, this seems wrong, but we need to return something
}

/*
	Return the source host.
*/
func (g *Gate) Get_src( ) ( *Endpt ) {
	if g != nil {
		return g.src
	}

	return nil
}

/*
	Return the dest host.
*/
func (g *Gate) Get_dest( ) ( *Endpt ) {
	if g != nil {
		return g.dest
	}

	return nil
}

/*
	Return the bandwidth
*/
func (g *Gate) Get_bandw( ) ( int64 ) {
	if g != nil {
		return g.bandw
	}

	return 0
}

/*
	Return the name of the switch that is attached.
*/
func (g *Gate) Get_sw_name( ) ( *string ) {
	if g != nil {
		return g.gsw.Get_id()
	}

	return nil
}

/*
	Build an spq struct based on the gate info
*/
func (g *Gate) Get_spq( qid *string, timestamp int64 ) ( *Spq ) {
	if g != nil {
		if g.gsw != nil {
			//id := g.gsw.Get_id()
			//func (l *Link) Get_forward_info( qid *string, tstamp int64 ) ( swid string, port int, queue int ) {
			return Mk_spq( g.gsw.Get_link( 0 ).Get_forward_info( qid, timestamp ) )
			//return Mk_spq( *id, -128, g.queue )
		}
	}

	return Mk_spq( "", 0, 0 )
}


// ------------------- link management -------------------------------------------------------------

/*
	A gate is used to control outbound traffic from a VM and so the gate "manages" all of the
	links which attach to the switch:

			| VM |-----[ switch ] -------- [Top of rack1]
                                 \-------- [Top of rack2]

	Each link between the switch and "top of rack" switches must have their utilisation managed
	with respect to the reservation.
*/

/*
	Increases the utilisation of the path by adding delta to all links. This assumes that each
	link has already been tested and indicated it could accept the change.  The return value
	does inidcate wheter or not the assignment was successful to all (true) or if one or more
	links could not be increased (false).
*/
func (g *Gate) Inc_utilisation( commence, conclude, delta int64, qid *string, ulimits *Fence ) ( state bool, err error ){
	if g == nil || g.gsw == nil {
		return false, fmt.Errorf( "nil pointer" )
	}

	if delta == 0 {					// don't waste time doing nothing
		return true, nil
	}

	state = true	
	i := 0
	for lnk := g.gsw.Get_link( i ); state == true && lnk != nil; lnk = g.gsw.Get_link( i ) {		// run all links
		if qid == nil {
			state, err = lnk.Inc_queue( qid, commence, conclude, delta, ulimits )
		} else {
			state = lnk.Inc_utilisation( commence, conclude, delta, ulimits )
		}

		if state {				// don't bump on failure so we can knock down if needed
			i++
		}
	}

	if ! state && delta > 0 {		// something failed, and we were increasing, must dec what we increased
		for i--; i >= 0; i-- {		// i points to the failed link, so back up first
			if qid == nil {
				 g.gsw.Get_link( i ).Inc_queue( qid, commence, conclude, -delta, ulimits )
			} else {
				 g.gsw.Get_link( i ).Inc_utilisation( commence, conclude, -delta, ulimits )
			}
		}
	} else {
		g.bandw += delta		// all good, record the change
		if g.bandw < 0 {
			g.bandw = 0
		}
	}

	return
}

/*
	Decrement the utilisation of the gate; see Inc_utilisation for details as this is just
	a wrapper passing a negative delta to that function.
*/
func (g *Gate) Dec_utilisation( commence, conclude, delta int64, qid *string, ulimits *Fence ) ( bool, error ){
	return g.Inc_utilisation( commence, conclude, -delta, qid, ulimits )
}

/*
	Add a queue to the links that are attached to the switch the gate references.
	This assumes that capacity for delta has been checked and all links can support
	it.
*/
func (g *Gate) Add_queue( commence, conclude, delta int64, qid *string, ulimits *Fence ) ( bool ){
	if g == nil || g.gsw == nil || qid == nil {
		return false
	}

	if delta == 0 {					// don't waste time doing nothing
		return true
	}

	i := 0
	for lnk := g.gsw.Get_link( i ); lnk != nil; lnk = g.gsw.Get_link( i ) {		// run all links
		err := lnk.Set_forward_queue( qid, commence, conclude, delta, ulimits )
		if err != nil {
			obj_sheep.Baa( 1, "gate/add_queue: %s: %s", lnk.Get_id(), err )
			return false
		}

		i++
	}

	return true
}

/*
	Make a change to the underlying queue associated with the qid passed in.
	Qid is the ID that was given to the queues when added to the links.
*/
func (g *Gate) Set_queue( qid *string, commence int64, conclude int64, delta int64, ulimits *Fence ) {
	if g == nil {
		return
	}

	i := 0
	for lnk := g.gsw.Get_link( i ); lnk != nil; lnk = g.gsw.Get_link( i ) {		// run all links attached to the switch
		err := lnk.Set_forward_queue( qid, commence, conclude, delta, ulimits )
		if err != nil {
			obj_sheep.Baa( 1, "gate/set_queue: %s: %s", lnk.Get_id(), err )
		}

		i++
	}
}

/*
	Checks to see if the switch can support the additional delta capacity. Returns true if it
	can and false otherwise.
*/
func (g *Gate) Has_capacity( commence int64, conclude int64, delta int64, usr *string, usr_max int64 ) ( bool ) {
	if g == nil || g.gsw == nil {
		return false
	}

	return g.gsw.Has_capacity_out( commence, conclude, g.bandw, usr, usr_max )
}

// ------------------------ string/json/human output functions ------------------------------------

/*
	Stringer interface function. Returns a string representation of the struct.
*/
func (g *Gate) String( ) ( s string ) {
	if g == nil {
		return ""
	}

	sip4, sip6 := g.src.Get_addresses()
	dip4, dip6 := g.dest.Get_addresses()
	return fmt.Sprintf( "%s %s %s %s %d %s", Safe_string( sip4 ), Safe_string( sip6 ), Safe_string( dip4 ), Safe_string( dip6 ), g.bandw, Safe_string( g.ext_ip ) )
}

/*
	Generates a string of json which represents the path.
*/
func (g *Gate) To_json( ) ( string ) {
	if g == nil {
		return "{ }"
	}
	sip4, sip6 := g.src.Get_addresses()
	dip4, dip6 := g.dest.Get_addresses()
	return fmt.Sprintf( `{ "srcip4": %q, "srcip6": %q, "destip4": %q, "destip6": %q, "bandw": %d "ext_ip": %s }`,
		Safe_string( sip4 ), Safe_string( sip6 ), Safe_string( dip4 ), Safe_string( dip6 ), g.dest, g.bandw, Safe_string( g.ext_ip ) )
}

