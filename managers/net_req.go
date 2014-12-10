// vi: sw=4 ts=4:

/*
	Mnemonic:	net_req.go
	Abstract:	Functions that manage net_req struct.
	Date:		16 November 2014
	Author:		E. Scott Daniels

	Mods:
*/

package managers

import (
	"codecloud.web.att.com/gopkgs/ipc"
)

type Net_vm  struct {
	name	*string
	id		*string			// openstack assigned id
	ip4		*string			// openstack assigned ip address
	ip6		*string			// openstack assigned ip address
	phost	*string			// phys host where vm is running
	mac		*string			// MAC
	fip		*string			// floating ip 
	gwmap	map[string]*string // the gate way information associated with the VM
}

/*
	Create a vm insertion structure. Not a good idea to create a nil named structure, but
	we'll allow it and subs in unnamed.
*/
func Mk_netreq_vm( name *string, id *string, ip4 *string, ip6 *string, phost *string, mac *string, fip *string, gwmap map[string]*string )  ( np *Net_vm ) {
	if name == nil {
		unv := "unnamed"
		name = &unv
	}

	np = &Net_vm {
		name: name,
		id: id,
		ip4: ip4,
		ip6: ip6, 
		phost: phost,
		mac: mac,
		fip: fip,
		gwmap: gwmap,			// we assume the map is ours to keep
	}

	return
}

/*
	Returns all values except the gateway map.
*/
func (vm *Net_vm) Get_values( ) ( name *string, id *string, ip4 *string, ip6 *string, phost *string, mac *string, fip *string ) {
	if vm == nil {
		return
	}

	return vm.name, vm.id, vm.ip4, vm.ip6, vm.phost, vm.mac, vm.fip
}

/*
	Returns the map.
*/
func (vm *Net_vm) Get_gwmap() ( map[string]*string ) {
	return vm.gwmap
}

/*
	Replaces the name in the struct with the new value if nv isn't nil;
*/
func (vm *Net_vm) Put_name( nv *string ) {
	if vm != nil  && nv != nil {
		vm.name = nv
	}
}

/*
	Replaces the id with the new value
*/
func (vm *Net_vm) Put_id( nv *string ) {
	if vm != nil {
		vm.id = nv
	}
}

/*
	Replaces the id with the new value
*/
func (vm *Net_vm) Put_ip4( nv *string ) {
	if vm != nil {
		vm.ip4 = nv
	}
}

/*
	Replaces the id with the new value
*/
func (vm *Net_vm) Put_ip6( nv *string ) {
	if vm != nil {
		vm.ip6 = nv
	}
}

/*
	Replace the physical host with the supplied value.
*/
func (vm *Net_vm) Put_phost( nv *string ) {
	if vm != nil {
		vm.phost = nv
	}
}

/*
	Send the vm struct to network manager as an insert to it's maps
*/
func  (vm *Net_vm) Add2graph( nw_ch chan *ipc.Chmsg ) {

	msg := ipc.Mk_chmsg( )
	msg.Send_req( nw_ch, nil, REQ_ADD, vm, nil )		
}
