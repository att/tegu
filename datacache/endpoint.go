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

	Mnemonic:	endpoint.go
	Abstract:	Functions specifically to support enpoints.

	Date:		19 November 2015 (broken out 02 Dec 2015 )
	Author:		E. Scott Daniels

	Mods:
*/

package datacache

import (
	"fmt"

	"github.com/gocql/gocql"			// cassandra db interface (requires go 1.4 or later)
)


/*
	Saves the endpoint information into the datacache.
	epm is assumed to be a copy of the map which includes port and router info as strings.
*/
func ( dc *Dcache ) Set_endpt( epid string, epm map[string]string ) ( err error ) {
	if dc == nil || dc.sess == nil {
		return fmt.Errorf( "no struct passed to set_endpt" )
	}

	if epid ==""  {
		return fmt.Errorf( "invalid endpoint id" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return err
		}
	}

	if epm != nil {
		dc.sheep.Baa( 1, "sending to datacache for endpoint: %s", epid )
    	err = dc.sess.Query( `INSERT INTO endpts (epid, epdata) VALUES (?, ?)`, epid, epm ).Exec()
    	if err != nil {
			dc.sheep.Baa( 1, "unable to set endpoint: key=%s: %s", epid, err )
			return err
    	}
	} else {
    	err = dc.sess.Query( `DELETE FROM endpts WHERE epid = ?`, epid ).Exec()
		if err != nil {
			dc.sheep.Baa( 1, "unable to delete endpoint: key=%s: %s", epid, err )
			return err
		} else {
			dc.sheep.Baa( 1, "endpoint deleted from datacache: %s", epid )
		}
	}

	return nil
}

/*
	Returns a list of endpoints that are currently in the datacache.
*/
func ( dc *Dcache ) Get_endpt_list( ) ( eplist []string, err error ) {
	var (
		epid	string
	)

	if dc == nil || dc.sess == nil {
		return nil, fmt.Errorf( "no struct passed to get_endpt_list" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return nil, err
		}
	}

	size := 64
	eplist = make( []string, 0, size )			// initial cap set at size, it will grow if needed
    iter := dc.sess.Query( `select  epid  from endpts` ).Consistency(gocql.One).Iter()
    for iter.Scan( &epid )  {
		eplist = append( eplist, epid )			// this will grow if we reach capacity so we must reassign
    }

	dc.sheep.Baa( 2, "%d endpoints exist in the datacache", len( eplist ) )
	return eplist[0:len( eplist )], nil
}

/*
	Fetch a single endpoint from the datacache. Returns a map.
*/
func ( dc *Dcache ) Get_endpt( epid string ) ( epm map[string]string, err error ) {
	var (
		epdata map[string]string
	)

	if dc == nil || dc.sess == nil {
		return nil, fmt.Errorf( "no struct passed to get_endpt" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return nil, err
		}
	}

	if epid == "" {
		return nil, fmt.Errorf( "invalid epid" )
	}

	err = dc.sess.Query( `SELECT epdata FROM endpts WHERE epid = ? LIMIT 1`, epid ).Consistency(gocql.One).Scan( &epdata )
    if err != nil {
		return nil, err
	}

	dc.sheep.Baa( 2, "pulled endpt from datacache: %d fields", len( epdata ) )

		
	return epdata, nil
}
