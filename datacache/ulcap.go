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

	Mnemonic:	ulcap.go
	Abstract:	The functions which specifically support user link caps.

	Date:		19 November 2015 (broken out 02 Dec 2015)
	Author:		E. Scott Daniels

	Mods:
*/

package datacache

import (
	"fmt"

	"github.com/gocql/gocql"			// cassandra db interface (requires go 1.4 or later)
)


/*
	Returns the current connectivity state and if not connected kicks our listener to try another
	time to connect.
*/
func ( dc * Dcache ) Is_connected( ) ( bool ) {
	if !dc.connected {
		ok, _ :=  dc.connect( )
		return ok
	}

	return true
}

/*
	Fetch the ulcap for the project from the datacache.
*/
func ( dc *Dcache ) Get_ulcap( project string ) ( pctg int, err error ) {

	pctg = 0

	if dc == nil || dc.sess == nil {
		return 0, fmt.Errorf( "nil ptr to struct passed or no session" )
	}

	if ! dc.connected {
		if ok, err :=  dc.connect(); ! ok {
			return 0, err
		}
	}

	err = dc.sess.Query( `SELECT pctg FROM ulcaps WHERE project = ? LIMIT 1`, project ).Consistency(gocql.One).Scan( &pctg )
    if err != nil {
		dc.sheep.Baa( 2, "unable to find ulcap for project: %s", project )
		err = fmt.Errorf( "ulcap not found for %s: %s", project, err )
    } else {
		dc.sheep.Baa( 2, "found ulcap in datacache: %s %d", project, pctg )
	}

	return pctg, err
}

/*
	Returns a map of all user limit capacities keyed by project id.
*/
func ( dc *Dcache ) Map_ulcaps( ) ( m map[string]int, err error ) {
	if dc == nil || dc.sess == nil {
		return nil, fmt.Errorf( "datacache struct was nil, or no session" )
	}

	var	(
		proj string
		pctg int
	)

	m = make( map[string]int, 64 )			// 64 is a hint not a hard limit

    iter := dc.sess.Query( `select project, pctg  from ulcaps` ).Consistency(gocql.One).Iter()
    for iter.Scan( &proj, &pctg )  {
		m[proj] = pctg
		dc.sheep.Baa( 2, "dug ulcap from datacache: %s = %d", proj, pctg )
    }

	return m, nil
}

/*
	Put the ulcap for the project into the datacache.
	Data is expected to be a string with project,value.
*/
func ( dc *Dcache ) Set_ulcap( project string, val int ) ( err error ) {

	if dc == nil || dc.sess == nil {
		return fmt.Errorf( "no struct passed to set_ulcap" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return err
		}
	}

	if val > 100 {
		val = 100
	} 

	if val >= 0 {
    	err = dc.sess.Query( `INSERT INTO ulcaps (project, pctg) VALUES (?, ?)`, project, val ).Exec()
    	if err != nil {
			dc.sheep.Baa( 2, "unable to set ulcap for project: %s", project )
    	}
	} else {
    	err = dc.sess.Query( `DELETE FROM  ulcaps WHERE project = ?`, project ).Exec()
    	if err != nil {
			dc.sheep.Baa( 2, "unable to delete user cap for project: %s: %s", project, err )
			return err
    	} else {
			dc.sheep.Baa( 1, "user cap for project was deleted: %s", project )
		}
	}

	return  nil
}
