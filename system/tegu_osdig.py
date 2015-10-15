#!/usr/bin/env python3
# :vi ts=4 sw=4:

'''
    Mnemonic:   os_digger
    Abstract:   Makes what ever openstack API calls are needed to complete the 
                command line request.  This isn't really indended for direct user
                use, but as a support tool for tegu_req.

    Requires:   At least: python3, nova client (python-novaclient), these may require
                additional things (with python it's bloody hard to tell).

                Basic syntax is:
                    os_digger [-v] command [parms]

                Supported commands are:
                    epid id [id2...idn]  -- translate id to an endpoint uuid
                                            id may be mac address, ip address or vm name.
                                            in the case of vm name the result may not be
                                            consistant or correct if the vm has multiple
                                            interfaces. If -v is set, then the output 
                                            is of the form  'id: uuid'. In either case,
                                            each uuid id printed on a separate line.

    Date:       14 Oct 2015
    Author:     E. Scott Daniels
'''


from novaclient import client
import sys
import os


def map_ifaces( ):
    '''
        Builds a map with keys that are mac addresses, ipaddresses and
        (maybe) VM names, all translating to the endpoint (port) uuid.
    '''
    with client.Client( "2", os.getenv( "OS_USERNAME" ), os.getenv( "OS_PASSWORD" ), os.getenv( "OS_TENANT_NAME" ), os.getenv( "OS_AUTH_URL" ) ) as ostack:
        vms = ostack.servers.list()                 # complete list of all VMs
        #print( dir( vms[9].interface_list()[0] ) )

        map = {}                                    # a hash that will map mac/ipaddr/name to endpoint uuid
        for vm in vms:
            ifs = vm.interface_list()               # get this VMs interface list
            vmname = vm.name
            for iface in ifs:
                portid = iface.port_id
                map[iface.mac_addr] = portid
                map[vmname] = portid                # probably bad form for the user to suss based on name as it's not predictable

                for ip_info in iface.fixed_ips:     # this is a list of hash, keys: ip_address, subnet_id
                    ipa = ip_info["ip_address"]
                    if ipa != None:
                        map[ipa] = portid
                    #end
                #end
            #end
        #end
    #end

    return map
#end


# --------------------- main processing -----------------------------------------------

argc = len( sys.argv ) 
verbose = False
argi = 1

while argi < argc and sys.argv[argi][0] == "-":
    if sys.argv[argi] == "-v":
        verbose = True
    else:
        print( "unrecognised option: %s" % sys.argv[argvi] )
        exit( 1 )
    #end

    argi += 1
#end

if argc - argi < 1:                     # must have at least one parm left
    print( "usage: os_digger command [parms]" )
    exit( 1 )

if sys.argv[argi] == "epid":
    if argc < 3:
        print( "usage: os_digger epid {mac|ip}" )
        exit( 1 )
    #end

    argi += 1
    map = map_ifaces( )
    rc = 0
    for i in range( argi, argc ):
        if sys.argv[i] in map:             # key is known
            if verbose:
                print( "%s: %s" % (sys.argv[i], map[sys.argv[i]]) )
            else:
                print( map[sys.argv[i]] )
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

else:
    print( "usage: os_digger %s is not a recognised command" % sys.argv[1] )
    exit( 1 )
#end

