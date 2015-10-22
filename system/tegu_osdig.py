#!/usr/bin/env python3
# :vi ts=4 sw=4:

'''
    Mnemonic:   os_digger
    Abstract:   Makes what ever openstack API calls are needed to complete the 
                command line request.  This isn't really indended for direct user
                use, but as a support tool for tegu_req.

    Requires:   At least: python3, nova client (python-novaclient), these may require
                additional things (with python it's bloody hard to tell).

    Date:       14 Oct 2015
    Author:     E. Scott Daniels
'''


from novaclient import client
import sys
import os


def map_ifaces( limit=None ):
    '''
        Builds a map with keys that are mac addresses, ipaddresses and
        (maybe) VM names, all translating to the endpoint (port) uuid.
        Also builds maps for vmname to all endpoints and endpoints to 
        all assigned ip addresses.

        Using a limiting vm name significatnly speeds this up as only 
        one interface list call is needed. If a limit is not provided
        the map will be for the entire project, and requires an interface
        info gathering api request for each VM. 
    '''
    with client.Client( "2", os.getenv( "OS_USERNAME" ), os.getenv( "OS_PASSWORD" ), os.getenv( "OS_TENANT_NAME" ), os.getenv( "OS_AUTH_URL" ) ) as ostack:
        vms = ostack.servers.list( )                 # complete list of all VMs

        map = {}                                    # a hash that will map mac/ipaddr/name to endpoint uuid
        all4vm = {}                                 # hash vm names to all of their endpoints
        port2ip = {}                                # hash an endpoint to one or more assigned ip addresses

        for vm in vms:
            if limit != None and limit != vm.name:
                continue

            ifs = vm.interface_list()               # get this VMs interface list
            vmname = vm.name
            for iface in ifs:
                portid = iface.port_id
                map[iface.mac_addr] = portid
                map[portid] = iface.mac_addr        # for all
                map[vmname] = portid                # probably bad form for the user to suss based on name as it's not predictable
                if vmname in all4vm:
                    all4vm[vmname] += " " + portid
                else:
                    all4vm[vmname] = portid

                for ip_info in iface.fixed_ips:     # this is a list of hash, keys: ip_address, subnet_id
                    ipa = ip_info["ip_address"]
                    if ipa != None:
                        map[ipa] = portid
                        if portid in port2ip:
                            port2ip[portid] += " " + ipa
                        else:
                            port2ip[portid] = ipa
                    #end
                #end
            #end
        #end
    #end

    return map, all4vm, port2ip
#end

def usage():
    print( '''
  Basic syntax is:
    os_digger [-v] command [parms]

  Supported commands are:
    [-a] epid id [id2...idn]  
        Translate id to an endpoint uuid id may be mac address, ip address or vm name.
        in the case of vm name the result may not be consistant or correct if the vm has multiple
        interfaces unless the -a option is used. When -a is supplied the output is a set of 
        tuples (epid, mac, ipaddress) for each interface that the VM has. If -v is set, then the 
        output is prefixed with the id used on the command line, and an output record for each
        id on the command line is generated.  In all cases, each ID's information is written
        on a separate line.
''' )


# --------------------- main processing -----------------------------------------------

argc = len( sys.argv ) 
verbose = False
print_all = False
argi = 1
limit = None

while argi < argc and sys.argv[argi][0] == "-":
    if sys.argv[argi] == "-l":
        argi += 1
        limit = sys.argv[argi]
    elif sys.argv[argi] == "-a":
        print_all = True
    elif sys.argv[argi] == "-v":
        verbose = True
    elif sys.argv[argi] == "-?" or sys.argv[argi] == "--help":
        usage()
        exit( 0 )
    else:
        print( "unrecognised option: %s" % sys.argv[argi] )
        exit( 1 )
    #end

    argi += 1
#end

if argc - argi < 1:                     # must have at least one parm left
    print( "usage: os_digger command [parms]" )
    usage()
    exit( 1 )

if sys.argv[argi] == "epid":
    if argc < 3:
        print( "usage: os_digger epid {vmname|mac|ip}" )
        usage()
        exit( 1 )
    #end

    argi += 1
    map, all4vm, port2ip = map_ifaces( limit )
    rc = 0
    for i in range( argi, argc ):
        if sys.argv[i] in map:             # key is known
            epid = map[sys.argv[i]]
            if print_all:
                str = ""
                if sys.argv[i] in all4vm:                   # vm name given, output by vmname rather than epid
                    for ep in all4vm[sys.argv[i]].split( " " ):
                        mac = map[ep]
                        if ep in port2ip:
                            ip = port2ip[ep]
                        else:
                            ip = "unknown"
                        str += "%s,%s,%s " % (ep, mac, ip)
                else:
                    mac = map[epid]
                    if epid in port2ip:
                        ip = port2ip[epid]
                    else:
                        ip = "unknown"

                    str = "%s,%s,%s" % (epid, mac, ip)
                #end
            else:
                str = epid
    
            if verbose:
                print( "%s: %s" % (sys.argv[i], str ) )
            else:
                print( str )
            #end
        else:
            if verbose:
                print( "%s: missing" % (sys.argv[i]) )
            else:
                rc = 1
            #end
        #end
    #end
    exit( rc )

if sys.argv[argi] == "dumpvm":
    map, all4vm, port2ip = map_ifaces( )
    for k in map:
        print( k, map[k] )

else:
    print( "usage: os_digger %s is not a recognised command" % sys.argv[argi] )
    usage()
    exit( 1 )
#end

