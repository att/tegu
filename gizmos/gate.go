// vi: sw=4 ts=4:

/*

	Mnemonic:	gate
	Abstract:	Manages a  gate which supports an ingress gate where we can 
				potentially supply rate limiting in the outbound direction
				from src-host to dest-host. 

	Date:		09 June 2015
	Author:		E. Scott Daniels

	Mod:
*/

package gizmos

import (
	"fmt"
	"strings"
)

type Gate struct {
	gsw 	*Switch			// the switch on which the gate is applied (to which src is attached)
	src		*Host			// source host for flow-mods etc.
	dest	*Host			// destination host for flow-mods etc. (nil if dest is cross project/external
	ext_ip	*string			// the external IP address (we won't have dest host if external)
	bwidth	int64			// amount of bandwidth to be used by the gate (if rate limiting)
	queue	int				// queue assigned by network
}

// ---------------------------------------------------------------------------------------

/*
	Creates a gate structure representing gated traffic from the source
	host to the dest host.  The switch is the switch that the source
	is attached to and where any gating flow-mods are going to be set.
*/
func Mk_gate( src *Host, dest *Host, gsw *Switch, bwidth int64 ) ( g *Gate ) {
	g = &Gate {
		src:		src,
		dest:		dest,
		gsw:		gsw,
		bwidth: 	bwidth,
		queue:		0,
	}

	return
}

/*
	Set the amount of bandwith that has been reserved along this path.
*/
func (g *Gate) Set_bandwidth( bw int64 ) {
	if( bw > 0 ) {
		g.bwidth = bw;
	}

	return
}

/*
	Set the queue
*/
func (g *Gate) Set_queue( q int ) {
	if g != nil {
		g.queue = q
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
func (g *Gate) Get_src( ) ( *Host ) {
	if g != nil {
		return g.src
	}

	return nil
}

/*
	Return the dest host.
*/
func (g *Gate) Get_dest( ) ( *Host ) {
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
		return g.bwidth
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
func (g *Gate) Get_spq( ) ( *Spq ) {
	if g != nil {
		if g.gsw != nil {
			id := g.gsw.Get_id()
			return Mk_spq( *id, -128, g.queue ) 
		}
obj_sheep.Baa( 1, ">>>> giz gate gsw is nil " )
	}

	return Mk_spq( "", 0, 0 )
}

// ------------------------ string/json/human output functions ------------------------------------

/*
	Generates a string representing the path.
*/
func (g *Gate) To_str( ) ( s string ) {
	return g.String()
}

/*
	Implement stringer for fmt.
*/
func (g *Gate) String( ) ( s string ) {
	if g == nil {
		return ""
	}

	sip4, sip6 := g.src.Get_addresses()
	dip4, dip6 := g.dest.Get_addresses()
	return fmt.Sprintf( "%s %s %s %s %d %d %s", Safe_string( sip4 ), Safe_string( sip6 ), Safe_string( dip4 ), Safe_string( dip6 ), g.bwidth, g.queue, Safe_string( g.ext_ip ) ) 
}

/*
	Generates a string of json which represents the path.
*/
func (g *Gate) To_json( ) ( string) {
	if g == nil {
		return "{ }"
	}
	sip4, sip6 := g.src.Get_addresses()
	dip4, dip6 := g.dest.Get_addresses()
	return fmt.Sprintf( `{ "srcip4": %q, "srcip6": %q, "destip4": %q, "destip6": %q, "bandw": %d "queue": %d, "ext_ip": %s }`, 
		Safe_string( sip4 ), Safe_string( sip6 ), Safe_string( dip4 ), Safe_string( dip6 ), g.dest, g.bwidth, g.queue, Safe_string( g.ext_ip ) ) 
}

