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

	Mnemonic:	pledge interface
	Abstract:	Defines what constitutes a pledge interface.
				Implemented by pledge_bw, pledge_mirror, pledge_steer and
				maybe others.

				Functions defined by the interface should make sense for ALL
				pledge types.  If they don't then the type(s) that require
				them should implement them and the user will need to convert
				the more generic interface type to the specific type to invoke
				the function needed. Examples of this which have been
				specifically omitted: Get_values(), Clone(), Set_path_list.

				There are also some generic functions such as json2pledge().
	Date:		21 May 2015
	Author:		E. Scott Daniels

	Mods:		16 Aug 2015 - listed funcs provided by Pledge_base, and those that must be written per Pledge type
*/

package gizmos

import (
	"fmt"
	"encoding/json"
)

/*
	This is the interface that all Pledge types must implement.
	Most of these functions have a default implementation in Pledge_base.
 */
type Pledge interface {
	// The following are implemented by Pledge_base
	Concluded_recently( window int64 ) ( bool )
	Commenced_recently( window int64 ) ( bool )
	Get_id( ) ( *string )
	Get_window( ) ( int64, int64 )
	Is_active( ) ( bool )
	Is_active_soon( window int64 ) ( bool )
	Is_expired( ) ( bool )
	Is_extinct( window int64 ) ( bool )
	Is_pending( ) ( bool )
	Is_pushed( ) (bool)
	Is_paused( ) ( bool )
	Is_valid_cookie( c *string ) ( bool )
	Pause( bool )
	Reset_pushed( )
	Resume( bool )
	Set_expiry( expiry int64 )
	Set_pushed()

	// The following must be implemented by each separate Pledge type
	Equals( *Pledge ) ( bool )
	Get_hosts() ( *string, *string )
	Has_host( *string ) ( bool )
	Nuke()
	String() ( string )
	To_chkpt( ) ( string )
	To_json( ) ( string )
	To_str() ( string )

	//Set_matchv6( bool )
	//Get_ptype( ) ( int )		users should use assertion or type determination in switch for these
	//Is_ptype( kind int ) ( bool )					// kind is one of the PT constants
}

// generic struct to unpack any type of pledge in order to determine the type
// This must only contain fields that exist in all pledge types, and only
// the fields that are needed to determine the type.
type J2p struct {
	Ptype	*int
}

/*
	Given a string that contains valid json, unpack it and examine the ptype.
	Based on ptype, unserialize into the appropriate object type and return.
	This used to be called Json2pledge(), but that was not an accurate name, given
	that we now store non-pledge objects in the checkpoint file.
*/
func UnserializeObject( jstr *string ) ( p *interface{}, err error ) {
	var pi interface{}

	jp := new( J2p )
	err = json.Unmarshal( []byte( *jstr ), &jp )
	if err == nil {
		if jp.Ptype != nil {
			switch *jp.Ptype {
				case PT_BANDWIDTH:
					bp := new( Pledge_bw )
					bp.From_json( jstr )
					pi = Pledge( bp )			// convert to interface type

				case PT_OWBANDWIDTH:			// one way bandwidth
					obp := new( Pledge_bwow )
					obp.From_json( jstr)
					pi = Pledge( obp )
	
				case PT_STEERING:
					mp := new( Pledge_steer )
					mp.From_json( jstr )
					pi = Pledge( mp )			// convert to interface type
	
				case PT_MIRRORING:
					mp := new( Pledge_mirror )
					mp.From_json( jstr )
					pi = Pledge( mp )			// convert to interface type
					
				case PT_CHAIN:
					cp, e2 := Mk_Chain( []byte( *jstr ) )
					pi = Pledge( cp )
					if e2 != nil {
						err = e2	// stupid go!
					}
					
				case PT_GROUP:
					cp, e2 := Mk_PortGroup( []byte( *jstr ) )
					pi = cp
					if e2 != nil {
						err = e2	// stupid go!
					}

				case PT_FC:
					cp, e2 := Mk_FlowClassifier( []byte( *jstr ) )
					pi = cp
					if e2 != nil {
						err = e2	// stupid go!
					}

				default:
					err = fmt.Errorf( "unknown pledge type in json: %d: %s", *jp.Ptype, *jstr )
					return
			}
		} else {
			err = fmt.Errorf( "no ptype found in json, unable to convert to pledge: %s", *jstr )
		}
	}

	p = &pi
	return
}
