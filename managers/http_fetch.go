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

	Mnemonic:	http_get
	Abstract:	Module contains function(s) needed by the http_api manager to process get
				requests. 

	Date:		10 December 2015
	Author:		E. Scott Daniels

	Mods:
*/

package managers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

/*
	Deal with a get request, but not quite in the traditional manner.  We expect 
	the 'filename' to be a generic name and we'll determine where to locate the 
	file (likely /usr/bin).  The purpose of this is to allow a user to pull the
	rjprt and tegu_req programmes which were distributed with this version of 
	Tegu, and not a general purporse http server.  Thus we will only recognise
	specific filenames, and rject all other attempts to get something.
*/
func parse_get( out http.ResponseWriter, uri string, sender string, xauth string ) (state string, msg string) {

	dir := "/usr/bin"

	tokens := strings.Split( uri, "/" )
	req_name := tokens[len( tokens )-1]

	fname := ""
	otype := "application/binary"
	switch  req_name {								// we only allow a fetch of just a few....
		case "rjprt":
			fname = dir + "/" + req_name

		case "tegu_req":
			fname = dir + "/" + req_name

		default:
			hdr := out.Header()
			hdr.Add("Content-type", "text/html")
			out.WriteHeader( 401 )
			now := time.Now();
			fmt.Fprintf( out, `<html><head></head><body style="background: black; color: #00ff90;"> %s sorry Charlie, not allowed</body></html>` + "\n", now );

			return "ERROR", "not allowed"
	}

	http_sheep.Baa( 1, "get sending file: %s", fname )
	f, err := os.Open( fname )
	count := 0
	if err == nil {
		defer f.Close()

		buffer := make( []byte, 4096 )
		state = "OK";
		msg = "ok";

		hdr := out.Header()
		hdr.Add( "Content-type", otype )

		for {
			nread, err := f.Read( buffer )
			count += nread

			if err != nil {
				if err != io.EOF {
					http_sheep.Baa( 1, "get error reading file: %s: %s", fname, err )
				} else {
					msg = fmt.Sprintf( "%d bytes transferred", count )
				}
				return
			}
	
			if nread > 0 {
				_, err = out.Write( buffer[0:nread] )
				if err != nil { 
					http_sheep.Baa( 1, "get error writing file: %s: %s", fname, err )
					msg = fmt.Sprintf( "%d bytes transferred", count )
					return
				}
			}
		}
	} else {
		http_sheep.Baa( 1, "get error opening file: %s: %s", fname, err )
		hdr := out.Header()
		hdr.Add("Content-type", "text/html")
		out.WriteHeader( 400 )
		now := time.Now();
		fmt.Fprintf( out, `<html><head></head><body style="background: black; color: #00ff90;"> %s read error, unable to find: %s</body></html>` + "\n", now, uri );
		state = "ERROR";
		msg = fmt.Sprintf( "cannot open file: %s: %s", fname, err )
		return
	}

	state = "OK";		// shouldn't get here, but prevent bad things
	msg = "ok";
	return;
}



