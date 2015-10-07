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
	Mnemonic:	sfs_mgr
	Abstract:	This is the scalable flow steering manager.  It is responsible for
                building/maintaining the SFS .data/.plan files, as well as periodically
                requesting the output from "ovs_sp2uuid -a" from every physical OpenStack host.

	Date:		30 Aug 2015
	Author:		Robert Eby
	Mods:		30 Aug 2015 - Created.
*/

package managers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

const (
	sfscmd			string = "/usr/bin/tegu_sfs"				// script to generate .plan files

	portdir			string = "/var/lib/tegu/sfs"				// where port_data files go
	plandir			string = "/var/lib/tegu/sfs"				// where SFS .data and .plan files go
	portlistfile	string = "/var/lib/tegu/sfs/port_list.json"	// JSON output from GET /v2.0/ports
	portlisttmpfile	string = "/var/lib/tegu/sfs/port_list.tmp"	// JSON output from GET /v2.0/ports (tmp file)

	ONEHOUR			int64 = int64(time.Hour) / int64(time.Second)
	ONEDAY			int64 = 24 * ONEHOUR
	ONEYEAR			int64 = 365 * ONEDAY
)

/*
	Start the Sfs_manager goroutine.   This handles background tasks associated with the
	scalable flow steering (SFS) parts of Tegu.  These include:
	1. Periodically fetching switch info from each host in the OpenStack universe
       (via /usr/bin/ovs_sp2uuid -a -h <host>)
	2. Periodically fetching port info from neutron port-list
	3. Building .data and .plan files for new flow chains, as they are defined.
	4. Updating .data and .plan files as needed, as the result of network changes or
       updates to the groups in the chains.

	The dependencies are:
	a. If any port_data file is changed, update SFS_MACMAP
	b. If SFS_MACMAP, port_list, or chain objects are changed, rebuild the .data file for chain.
	c. If .data file is changed, rebuild the plan file for chain.
	d. If .plan file is changed, and chain is active, re-push the plan
 */
func Sfs_manager( inch chan *ipc.Chmsg ) {
	sfs_logger = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	sfs_logger.Set_prefix( "sfs" )
	tegu_sheep.Add_child( sfs_logger )					// we become a child so that if the master vol is adjusted we'll react too

	var last_cleanup int64 = 0
	var all_sys_up bool = false

	// Perform periodic tasks every 15 min.
	// 1. Fetch ovs_sp2uuid -a output
	// 2. Fetch neutron port-list output
	// 3. Cleanup old files once an hour
	tklr.Add_spot(     5, inch, REQ_PUSH, nil, 1 )
	tklr.Add_spot( 15*60, inch, REQ_PUSH, nil, ipc.FOREVER )

	sfs_logger.Baa( 3, "sfs_manager is running %x", inch )
	cpr := ipc.Mk_chmsg( )
//	my_ch := make( chan *ipc.Chmsg )
	for {
		// Process next message
		msg := <- inch					// wait for next message
		sfs_logger.Baa( 3, "processing message: %d", msg.Msg_type )

		switch msg.Msg_type {
		// add commands to process new post/put/delete
		case REQ_GENPLAN:
			// Request to generate a plan file for a new/updated Chain
			req := msg.Req_data.( *gizmos.Chain )
			buildOrUpdatePlan( req )
			msg.Response_data = nil
			cpr.Send_req( rmgr_ch, nil, REQ_PUSHNOW, nil, nil )

		case REQ_ALLUP:
			sfs_logger.Baa( 0, "sfs_manager: all systems up!" )
			all_sys_up = true

		case REQ_PUSH:
			// Push request to agent to get "ovs_sp2uuid -a" output from each host.
			// Do not wait for a response (agent will put this output into files).
			cpr.Send_req( am_ch, nil, REQ_SWITCHINFO, nil, nil )

			// Also ask OS manager to get neutron port list
			/* not working yet :-(
			stupid_go := portlisttmpfile
			cpr.Send_req( osif_ch, my_ch, REQ_PORTINFO, &stupid_go, nil )
			req := <- my_ch
			if req.State == nil {
				if fileExists( portlisttmpfile ) {
					sfs_logger.Baa( 1, "updated port list received" )
					if !fileExists( portlistfile ) || !filesEqual( portlisttmpfile, portlistfile ) {
						os.Remove(portlistfile)
						os.Link(portlisttmpfile, portlistfile)

						// Port list changed - trigger update of all .data files
						// Note: this really should just update the SFSMAP, and if that changes, then the data files should change
						sfs_logger.Baa( 1, "port list has changed .. regenerating data files" )
						for _, v := range gizmos.Chains {
							buildOrUpdatePlan( v )
						}
					}
					os.Remove(portlisttmpfile)
				}
			}*/

			// Also, cleanup old .plan/.data files once per hour
			if all_sys_up && (time.Now().Unix() - last_cleanup > ONEHOUR) {
				cleanup()
				last_cleanup = time.Now().Unix()
			}

		default:
			sfs_logger.Baa( 0, "WRN: Sfs_manager: unknown message: %d [TGURMG001]", msg.Msg_type )
			msg.State = fmt.Errorf( "sfs_manager: unknown message: %d", msg.Msg_type )
			msg.Response_data = nil
			msg.Response_ch = nil				// we don't respond to these.
		}
		sfs_logger.Baa( 3, "processing message complete: %d", msg.Msg_type )

		// if a response channel was provided, send our result back to the requester
		if msg.Response_ch != nil {
			msg.Response_ch <- msg
		}
	}
}
/*
	This function reads the JSON provided by the Neutron API "GET /v2.0/ports" and returns an array
	of maps, one map per port/subnet/IP combo.
 */
func ReadPortListJSON( dat map[string]interface{} ) []*map[string]string {
	rv := make ( []*map[string]string, 0 )
	ports := dat["ports"].( []interface{} )
	for ix := 0; ix < len(ports); ix++ {
		hash := ports[ix].( map[string]interface{} )
		if _, ok := hash["id"]; ok {
			if _, ok := hash["mac_address"]; ok {
				if _, ok := hash["tenant_id"]; ok {
					if _, ok := hash["fixed_ips"]; ok {
						uuid   := hash["id"         ].(string)
						mac    := hash["mac_address"].(string)
						tenant := hash["tenant_id"  ].(string)
						iplist := hash["fixed_ips"  ].([]interface{})
						for jx := 0; jx < len(iplist); jx++ {
							hash2 := iplist[jx].( map[string]interface{} )
							if _, ok := hash2["ip_address"]; ok {
								if _, ok := hash2["subnet_id"]; ok {
									ipaddr := hash2["ip_address"].(string)
									subnet := hash2["subnet_id" ].(string)
									nmap := map[string]string {
										"uuid":      strings.ToLower(uuid),
										"mac":       strings.ToLower(mac),
										"ip":        strings.ToLower(ipaddr),
										"subnet":    strings.ToLower(subnet),
										"tenant_id": strings.ToLower(tenant),
									}
									rv = append( rv, &nmap )
								}
							}
						}
					}
				}
			}
		}
	}
	return rv
}

/*
	Returns an array of maps, one map entry for each port this tenant owns.
 */
func GetTenantPorts(tenant_id string) []*map[string]string {
	rv := make ( []*map[string]string, 0 )
	rv2, err := ReadPortListFile(portlistfile)
	tenant_id = strings.ToLower(tenant_id)
	if err == nil {
		for _, v := range rv2 {
			if tenant_id == (*v)["tenant_id"] {
				rv = append(rv, v)
			}
		}
	}
	return rv
}

/*
	Clean up old .data/.plan files
 */
func cleanup() {
	n := 0
	sfs_logger.Baa( 0, "Cleaning up old files" )
	dp, err := os.Open(plandir)
	if err == nil {
		defer dp.Close()
		names, err := dp.Readdirnames(-1)
		if err == nil {
			for _, name := range names {
				if len(name) > 9 && name[0:9] == "tegu.sfs." {
					fullname := fmt.Sprintf("%s/%s", plandir, name)
					parts := strings.Split(name, ".")
					chain_id := parts[2]
					if gizmos.Chains[chain_id] == nil {
						sfs_logger.Baa( 1, "Removing old plan/data file %s", name )
						os.Remove( fullname )
						n++
					}
				}
			}
		}
	}
	sfs_logger.Baa( 1, "Cleanup done, %d files removed.", n )
}

/*
	Get all the data need to populate a data file
	(SFS_RULES, SFS_ONEWAY, SFS_SRCSET, SFS_DSTSET, SFS_SETS, SFS_EXPIRATION and SFS_MACMAP).
	Then run the script to generate the plan shell script.  Data files and plans are kept in:
		/var/lib/tegu/sfs/tegu.sfs.<uuid>.data (data files)
		/var/lib/tegu/sfs/tegu.sfs.<uuid>.plan (plan files)
 */
func buildOrUpdatePlan(req *gizmos.Chain) (err error) {
	tfile := fmt.Sprintf( "%s/tegu.sfs.%s.tmp",  os.TempDir(), req.Id )	// temp file
	dfile := fmt.Sprintf( "%s/tegu.sfs.%s.data", plandir, req.Id )		// data file
	pfile := fmt.Sprintf( "%s/tegu.sfs.%s.plan", plandir, req.Id )		// plan file

	// (re)build the data file
	err = buildDataFile(req, tfile )
	if err == nil {
		sfs_logger.Baa( 1, "http_chain: built data file: %s", dfile )

		// Do we need to (re)build the plan file?
		if !fileExists( dfile ) || !filesEqual( tfile, dfile ) {
			if fileExists( plandir ) {
				// remove data and plan files
				os.Remove( dfile )
				os.Remove( pfile )
			} else {
				// make plan dir if it does not exist
				os.Mkdir( plandir, 0775 )
			}
			copyFile( tfile, dfile )

			// make the plan
			err = buildPlanFile( dfile, tfile )
			if err == nil {
				sfs_logger.Baa( 1, "http_chain: built plan file: %s", pfile )
				copyFile( tfile, pfile )
			} else {
				sfs_logger.Baa( 1, "http_chain: did not build plan file: %s", err.Error() )
			}
		}
	} else {
		sfs_logger.Baa( 1, "http_chain: did not build data file: %s", err.Error() )
	}
	os.Remove( tfile )	// delete temp file
	return
}
/*
	This builds the .data file for the chain that is passed as an argument.
 */
func buildDataFile(req *gizmos.Chain, file string ) (err error) {
	if len(req.Classifiers) == 0 {
		err = fmt.Errorf("No flow classifiers in this chain?!?")
		return
	}
	fcarray := make( []*gizmos.FlowClassifier, 0)
	for _, s := range req.Classifiers {
		fc := gizmos.Classifiers[s]
		if fc == nil {
			err = fmt.Errorf("Cannot find flow classifier with ID: "+s)
			return
		}
		fcarray = append(fcarray, fc)
	}
	fc, err := gizmos.MergeClassifiers(fcarray)
	if err != nil {
		return
	}
	rules, revrules, err := fc.BuildOvsRules()
	if err != nil {
		return
	}
	tports := GetTenantPorts( req.Tenant_id )
	sourcemac, err := getMacForIP(tports, fc.Source_ip)
	if err != nil {
		return
	}
	destmac, _ := getMacForIP(tports, fc.Dest_ip)		// destmac is allowed to be ""
	pmap, err := buildPortMap()
	if err != nil {
		return
	}

	from, to := req.Get_window()
	now := time.Now().Unix()
	if from < now {
		from = now
	}
	expiration := to - from
	bs := bytes.NewBufferString("")
	bs.WriteString("#----------------------------------------------------------------------------------------------\n")
	bs.WriteString("# Environment used to generate the steering plan for chain "+req.Id+"\n")
	bs.WriteString("#----------------------------------------------------------------------------------------------\n")
	bs.WriteString("SFS_RULES='" + rules + "'\n")
	bs.WriteString("SFS_REV_RULES='" + revrules + "'\n")
	bs.WriteString("SFS_ONEWAY=false\n")
	if expiration <= ONEYEAR {
		// If > 1 year, then we must manually remove the chain from OVS
		bs.WriteString("SFS_EXPIRATION=" + strconv.FormatInt(expiration, 10) + "\n")
	}
	bs.WriteString("SFS_SRCSET=set_src\n")
	bs.WriteString("set_src='" + sourcemac + "'\n")
	bs.WriteString("SFS_DSTSET=set_dest\n")
	bs.WriteString("set_dest='" + destmac + "'\n")
	layer := 0
	sets := ""
	for _, g := range req.Groups {
		layer++
		sets = fmt.Sprintf("%s set%d", sets, layer)
		grp := gizmos.PortGroups[g]
		if grp == nil {
			err = fmt.Errorf("Whoops: no group with ID "+g)
			return
		}
		portmacs := ""
		for _, pp := range grp.Ports {
			entry := pmap[pp.Ingress]	// TODO handle egress port as well
			if entry == nil && pp.Ingress[0:24] == "00000000-0000-0000-0000-" {	// temp hack?
				t := pp.Ingress[24:]
				t  = fmt.Sprintf("%s:%s:%s:%s:%s:%s", t[0:2], t[2:4], t[4:6], t[6:8], t[8:10], t[10:12])
				entry = &portmapentry {
					mac: t,
				}
			}
			if entry == nil {
				err = fmt.Errorf( "Whoops: no port for UUID "+ pp.Ingress)
				return
			}
			portmacs = fmt.Sprintf("%s %s", portmacs, entry.mac)
		}
		bs.WriteString(fmt.Sprintf("set%d='%s'\n", layer, strings.TrimSpace(portmacs)))
	}
	bs.WriteString("SFS_SETS='"+strings.TrimSpace(sets)+"'\n")
	bs.WriteString("\n")
	bs.WriteString("# Map of MAC -> port UUID, physical host, switch port number\n")
	bs.WriteString("SFS_MACMAP='\n")
	for _, k := range sortkeys ( pmap ) {
		e := pmap[k]
		bs.WriteString( fmt.Sprintf("%s/%s/%s/%s/%s\n", e.mac, e.uuid, e.host, e.portnum, e.intfc) )
	}
	bs.WriteString("'\n")

	// Write to file
	f, err := os.Create(file)
	if err == nil {
		_, err = f.WriteString(bs.String())
		f.Close()
	}
	return
}

func sortkeys ( m map[string]*portmapentry ) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func getMacForIP(ports []*map[string]string, s string) (string, error) {
// TODO s could be a CIDR in which case return a list
	for k := range ports {
		v := *ports[k]
		if v["ip"] == s {
			return v["mac"], nil
		}
	}
	return "", fmt.Errorf("Cannot find MAC for IP: " + s)
//	my_ch := make( chan *ipc.Chmsg )
//	defer close ( my_ch )

//	req := ipc.Mk_chmsg( )
//	req.Send_req( osif_ch, my_ch, REQ_IP2MAC, &s, nil )			// request IP -> MAC translation
//	req = <- my_ch
//	if req.Response_data == nil {
//		err = fmt.Errorf("Cannot find MAC for IP: " + s)
//	} else {
//		rv = *req.Response_data.(*string)
//	}
//	return
}

//type SortIfc	[]string
//func (a SortIfc) Len() int           { return len(a) }
//func (a SortIfc) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
//func (a SortIfc) Less(i, j int) bool { return a[i] < a[j] }

type portmapentry struct {
	mac		string
	uuid	string
	host	string
	portnum	string
	intfc	string
}

func buildPortMap() (m map[string]*portmapentry, err error) {
	dp, err := os.Open(portdir)
	if err == nil {
		defer dp.Close()
		names, err := dp.Readdirnames(-1)
		if err == nil {
			m = make( map[string]*portmapentry )
			for _, name := range names {
				if len(name) > 10 && name[0:10] == "port_data." {
					fullname := fmt.Sprintf("%s/%s", portdir, name)
					hostname := name[10:]
					fp, err := os.Open(fullname)
					if err == nil {
						defer fp.Close()
						incl := false
						scanner := bufio.NewScanner(fp)
						scanner.Split(bufio.ScanLines)
						for scanner.Scan() {
							line := scanner.Text()
							if line[0:7] == "switch:" {
								incl = strings.Index(line, "br-int") >= 0
							}
							if line[0:5] == "port:" && incl {
								parts := strings.Split(line, " ")
								if len(parts) == 7 && parts[4] != "" {
									entry := portmapentry {
										mac:     parts[4],
										uuid:    parts[5],
										host:    hostname,
										portnum: parts[2],
										intfc:   parts[3],
									}
									m[entry.uuid] = &entry
								}
							}
						}
					}
				}
			}
		} else {
			err = fmt.Errorf( "Readdirnames(): "+err.Error() )
		}
	} else {
		err = fmt.Errorf( "opening portdir: "+err.Error() )
	}
	return
}

func buildPlanFile( dfile string, ofile string ) (err error) {
	cmd := exec.Command(sfscmd, dfile)
	output, err := cmd.Output()
	if err == nil {
		f, err := os.Create(ofile)
		if err == nil {
			_, err = f.WriteString(string(output))
			f.Close()
		}
	}
	return
}

func fileExists( path string ) bool {
	_, err := os.Stat( path )
	return err == nil || os.IsExist(err)
}

// Check if two files are equal
func filesEqual( path1 string, path2 string ) bool {
	s1, err := os.Stat( path1 )
	if err == nil {
		s2, err := os.Stat( path2 )
		if err == nil {
			if s1.Size() == s2.Size() {
				// Sizes are the same, must check contents now :-(
				b1, e1 := ioutil.ReadFile( path1 )
				if e1 == nil {
					b2, e2 := ioutil.ReadFile( path2 )
					if e2 == nil {
						for i := 0; i < len(b1); i++ {
							if b1[i] != b2[i] {
								return false
							}
						}
						return true
					}
				}
			}
		}
	}
	return false
}

func ReadPortListFile(nm string) (rv []*map[string]string, err error) {
	bytes, err := ioutil.ReadFile( nm )
	if err == nil {
		// unmarshal into dat
		var dat map[string]interface{}
		err = json.Unmarshal(bytes, &dat)
		if err == nil {
			rv = ReadPortListJSON( dat )
		}
	}
	return
}

func copyFile( src string, dest string ) error {
    in, err := os.Open(src)
    if err == nil {
		defer in.Close()
		out, err := os.Create(dest)
		if err == nil {
			defer func() {
				cerr := out.Close()
				if err == nil {
					err = cerr
				}
			}()
			_, err = io.Copy(out, in)
			if err == nil {
				err = out.Sync()
			}
		}
    }
    return err
}
