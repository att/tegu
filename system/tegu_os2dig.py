#!/usr/bin/env python
# :vi ts=4 sw=4:
# bloody neutron doesn't have a python3.x version
'''
    Mnemonic:   os2_dig
    Abstract:   Bloody openstack neutron doesn't have a python3 interface so this is
                a step backwards to allow us to dig things from the network side.


    Requires:   At least: python2, neutron client, nova client (python-novaclient), these may require
                additional things (with python it's bloody hard to tell).

    Date:       22 Oct 2015
    Author:     E. Scott Daniels
'''

from neutronclient.v2_0 import client
import os
import sys


def map_routers( epid=None ):
    '''
        Dumps a bit of info about each router we can find:
            mac, network-id, ip
    '''
    username= os.getenv( "OS_USERNAME" ) 
    password= os.getenv( "OS_PASSWORD" )
    tenant_name= os.getenv( "OS_TENANT_NAME" )
    auth_url= os.getenv( "OS_AUTH_URL" ) 
    
    nif = client.Client(username=username, password=password, tenant_name=tenant_name, auth_url=auth_url)   # get an interface to neutron
    
    ports = nif.list_ports()["ports"]
    subnet_target = None
    if epid != None:
        for p in ports:
            if p["id"] == epid:
                subnet_target = p["network_id"]

    for p in ports:
        if p["device_owner"] == "network:router_interface":                 # dump out just routers
            for fip in p["fixed_ips"]:
                if subnet_target == None or p["network_id"] == subnet_target:
                    print p["mac_address"], p["network_id"], fip["ip_address"]
        #end
    #end

    return 0
#end


def usage():
    print( '''
  Basic syntax is:
    tegu_os2dig [-v] command [parms]

  Supported commands are:
    routers [endpoint-id]
        Dumps mac, net-id, ip-address for each router we can find. If the uuid of an 
        endpoint (port/interface/whatever openstack wants to call it today) is given
        then only the router(s) which live on that network are displayed.
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
    print( "usage: tegu_os2dig command [parms]" )
    usage()
    exit( 1 )

if sys.argv[argi] == "routers":
    if argc - argi > 1:
        epid = sys.argv[argi+1]
    else:
        epid = None

    rc = map_routers( epid )
    exit( rc )

else:
    print( "usage: tegu_os2dig %s is not a recognised command" % sys.argv[argi] )
    usage()
    exit( 1 )
#end

