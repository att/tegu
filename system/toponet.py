#!/usr/bin/env python
# encoding: utf-8
'''
toponet -- generate physical network topology from LLDP enabled 
           Cisco/Arista switches


@author:     Kaustubh Joshi

@copyright:  2014 AT&T Labs. All rights reserved.

@license:    license

@contact:    krj@research.att.com
@deffield    updated: Updated
'''

import sys
import os
import re

from collections import deque

import logging          # Logging
import configargparse   # Configuration parser
import paramiko         # SSH framework

from pysnmp.entity.rfc3413.oneliner import cmdgen
from novaclient.client import Client

#import argparse        # Old configuration parser

from __builtin__ import True

DEF_HOSTCAPACITY =   "1000000000"   # 1GB
DEF_SWITCHCAPACITY = "10000000000"  # 10GB

__all__ = []
__version__ = 0.1
__date__ = '2014-11-01'
__updated__ = '2014-11-01'

class Link:
    def __init__(self, src="", srcPort="-128", dst="", dstPort="-128", mlag=""):
        self.src = src
        self.dst = dst
        self.srcPort = srcPort
        self.dstPort = dstPort
        self.type = "internal"
        self.capacity = DEF_HOSTCAPACITY
        self.bidi = True
        self.mlag = mlag
        
    def __str__(self):
        output = '{\n'
        output += '\t"src-switch": "' + self.src + '",\n'
        output += '\t"src-port": ' + self.srcPort + ',\n'
        output += '\t"dst-switch": "' + self.dst + '",\n'
        output += '\t"dst-port": ' + self.dstPort + ',\n'
        output += '\t"type": "' + self.type + '",\n'
        if self.bidi:
            output += '\t"direction": "bidirectional",\n'
        output += '\t"capacity": ' + self.capacity
        if self.mlag != "":
            output += ',\n\t"mlag": "' + self.mlag + '"'
        output += '\n}'
        return output
    
        
class NetDataSource:
    def hasElement(self, name):
        return False
    
    def getElementList(self):
        return []

    def getElementLinks(self, name):
        return []
    

def normalizePortNum(port):
    ''' Converts portnumbers from either Arista or Cisco formats to an integer
        number. Arista uses single integer portnumbers, while Cisco uses
        the cardnum/ifnum format, e.g., 2/1
    '''
    portmatch = re.search('(\d+)/(\d+)', port)  
    if portmatch:
        return portmatch.group(1)+portmatch.group(2)
    else:
        return port
    
def normalizeHostname(hostname):
    ''' Normalize hostnames. Currently, tegu requires only hostnames
        because that is what Openstack host-list returns. Strip the 
        domainname if FQDN is read in.
    ''' 
    return hostname.split(".")[0]

def shortName(name):
    return name.split('@')[0].split('.')[0]
    

def parseLldpCtl(file, hosts, thisHost=None, ifacelist=set()):
    ''' Parse output from the lldpctl command executed on a single host
        or a set of hosts. If run on a single host, the name of the host
        must be provided in the thisHost parameter. If run across a set of
        hosts using a command like knife, the hostname is expected to be
        the first identifier on each line.
    '''
    iface_pat = re.compile('^(?P<host>\S+)?\s*Interface:\s+(?P<iface>[^,]+),\s+via:')
    call_table = {
        'dst': re.compile('SysName:\s+([^\s]+)'),
        'dstPort': re.compile('PortID:\s+ifname\s+Ethernet(\d+(?:/\d+)?)'),
        'mlag': re.compile('Port is aggregated. PortAggregID:\s+(\S+)')
    }
    current_link = None
    current_host = ""
    current_iface = ""
        
    while True:
        line = file.readline() 
        
        if not line:
            break
        
        new_iface_match = iface_pat.search(line)
        if new_iface_match:
            iface = new_iface_match.groupdict()['iface']
            if not ifacelist or (iface in ifacelist):
                if thisHost:
                    current_host = thisHost
                else:
                    current_host = new_iface_match.groupdict()['host']
                current_host = normalizeHostname(current_host)
                current_iface = iface
                current_link = Link(current_host + '@' + current_iface, "-128")
                current_link.capacity = DEF_HOSTCAPACITY
            else: # New link found, but we don't need it
                current_link = None             
        elif current_link and re.search('-----------', line):
            if current_link.src == "" or current_link.dst == "" or \
                current_link.srcPort == None or current_link.dstPort == None:
                logging.warning("Malformed link: " + str(current_link))
            current_link.dst = normalizeHostname(current_link.dst)
            current_link.dstPort = normalizePortNum(current_link.dstPort)
            current_link.mlag = "mlag_" + current_host
            host = hosts.setdefault(current_host, {})
            host[current_iface] = current_link
        elif current_link:
            for key, rule in call_table.iteritems():
                attr_match = rule.search(line)
                if attr_match:
                    setattr(current_link, key, attr_match.group(1))


class LldpctlFileLoader(NetDataSource):
    '''Loads interface data from a file that contains a dump of lldpctl
       command output from all the hosts in the system. Each line of the
       lldpctl output is assumed to be preceeded by the hostname.
       E.g., myhost.int.cci.att.com: Interface Eth2'''
       
    def __init__(self, lldpFilename):
        self.hosts = {}
        self.ifaces = set()
        self.lldpFilename = lldpFilename
        
    def getPortNum(self, port):
        portmatch = re.search('(\d+)/(\d+)', port)  
        if portmatch:
            return portmatch.group(1)+portmatch.group(2)
        else:
            return port
        
    def load(self):
        lldp_file = open(self.lldpFilename)
        parseLldpCtl(lldp_file, self.hosts, ifacelist=self.ifaces)
                    
    def addIface(self, ifname):
        self.ifaces.add(ifname)
            
    def hasElement(self, name):
        return (name in self.hosts)
    
    def getElementList(self):
        return self.hosts.keys()
    
    def getElementLinks(self, name):
        return self.hosts[name].values()
            
    
class SwitchFileLoader(NetDataSource):
    '''Loads interface data from a file that contains a dump of CLI command
       output from Arista and Cisco switches. Following assumptions made:
       a) The file can contain output from multiple switches
       b) The command prompt on the switch is switch_name#
       c) The following three commands are run
          i)   show interface status
          ii)  show port-channel sum
          iii) show lldp neigh
       d) The commands can be executed in any order'''
         
    speedmap = {"1G": "1000000000", "10G":"10000000000",
                "20G":"20000000000", "40G":"40000000000"}
        
    def __init__(self, switchFilename):
        self.switches = {}
        self.switchFilename = switchFilename
        

    def parseCisco(self, name, switch):
        
        links = re.findall('\n([^\s]+).*Eth(\d+(?:/\d+)?)\s+\d+\s+\S+' + \
                           '\s+Ethernet(\d+(?:/\d+)?)', switch['lldp'])

        #print(name + "(Cisco)")
        for linkmatch in links:
            new_link = Link(src=name, srcPort=normalizePortNum(linkmatch[1]),
                            dst=normalizeHostname(linkmatch[0]), 
                            dstPort=normalizePortNum(linkmatch[2]))
            #print("\t"+normalizeHostname(linkmatch[0]))
            # Read link full duplex and speed    
            speedmatch = re.search("Eth"+linkmatch[1]+"\s+.+connected\s+[^\s]+" + 
                                   "\s+([^\s]+)\s+([^\s]+)\s+", switch["ifaces"])
            
            if speedmatch and (speedmatch.group(2) in self.speedmap.keys()):
                new_link.bidi = (speedmatch.group(1) == "full")
                new_link.capacity = self.speedmap[speedmatch.group(2)]
            else:
                logging.warning("Couldn't find link " + str(new_link))
                
            if  new_link.capacity == None:
                new_link.capacity = DEF_SWITCHCAPACITY
            
            # Read link mlag
            mlagmatch = re.search("\n\d+\s+([^(]+)\(.+LACP|NONE.+Eth" + \
                                  linkmatch[1], switch['portchannel'])
            if mlagmatch:
                new_link.mlag = name + "_" + mlagmatch.group(1)
            switch_links = switch.setdefault('links', {}) 
            switch_links[new_link.srcPort] = new_link
          
        
    def parseArista(self, name, switch):
        
        links = re.findall('Et(\d+(?:/\d+)?)\s+([^\s]+).*Ethernet(\d+(?:/\d+)?)', 
                           switch['lldp'])

        #print(name + " (Arista)")
        for linkmatch in links:
            new_link = Link(src=name, srcPort=normalizePortNum(linkmatch[0]),
                            dst=normalizeHostname(linkmatch[1]), 
                            dstPort=normalizePortNum(linkmatch[2]))
            #print("\t"+normalizeHostname(linkmatch[1]))
                
            # Read link full duplex and speed    
            speedmatch = re.search("Et"+linkmatch[0]+"\s+.+connected\s+.*" +
                                  "(half|full)\s+([^\s]+)\s+", switch["ifaces"])

            if speedmatch and (speedmatch.group(2) in self.speedmap.keys()):
                new_link.bidi = (speedmatch.group(1) == "full")
                new_link.capacity = self.speedmap[speedmatch.group(2)]
            else:
                logging.warning("Couldn't find link " + str(new_link))
                logging.warning(linkmatch[0] + " didn't match: " + switch["ifaces"])

            if  new_link.capacity == None:
                new_link.capacity = DEF_SWITCHCAPACITY
            
            # Read link mlag
            mlagmatch = re.search("\n\s+([^(\n]+)\(.+LACP.+Et" + linkmatch[0], 
                                  switch['portchannel'])
            if mlagmatch:
                new_link.mlag = name + "_" + mlagmatch.group(1)
            switch_links = switch.setdefault('links', {}) 
            switch_links[new_link.srcPort] = new_link
            

    def loadChunk(self, the_file, end_tag):
        chunk = ""                
        done = False
        while not done:
            file_pos = the_file.tell()
            line = the_file.readline()
            end_tag_match = re.search(end_tag, line)
            if end_tag_match or not line:
                the_file.seek(file_pos)
                done = True
            else:
                chunk = chunk + line
        return chunk
    
    def loadSection(self, start_tag, key, the_file, line):
        match = re.search("(^[^#]+)#\s*"+start_tag, line)
        if (match):
            src_switch = match.group(1)
            switch = self.switches.setdefault(normalizeHostname(src_switch), {})
            switch[key] = self.loadChunk(the_file, '^'+src_switch+'#')
            return normalizeHostname(src_switch)
        return None
        
    def load(self):
        switch_file = open(self.switchFilename)
        
        # Parse the files
        while True:
            line = switch_file.readline()

            if not line:
                break

            if self.loadSection('show interface status', 'ifaces', switch_file, line):
                continue
            
            if self.loadSection('show port-channel sum', 'portchannel', switch_file, line):
                continue           
            
            src_switch = self.loadSection('show lldp neigh', 'lldp', switch_file, line)            
            if src_switch:
                #print src_switch
                if re.search('Capability codes:', self.switches[src_switch]['lldp']):
                    self.switches[src_switch]['type'] = 'Cisco'
                else:
                    self.switches[src_switch]['type'] = 'Arista'
        
        # Finished parsing file, now populate links    
        for name, switch in self.switches.iteritems():
            if switch['type'] == 'Cisco':
                self.parseCisco(name, switch)
            else:
                self.parseArista(name, switch)

            
    def hasElement(self, name):
        return (name in self.switches)
    
    def getElementList(self):
        return self.switches.keys()
    
    def getElementLinks(self, name):
        return self.switches[name]['links'].values()

    
class OpenStackHostLoader(NetDataSource):
    VERSION = 2
    
    def __init__(self, osUrl, osUname, osPwd, osTenant,
                 hUname, hPwd, hKeyFile=""):
        self.osUrl = osUrl
        self.osUname = osUname
        self.osPwd = osPwd
        self.osTenant = osTenant
        self.hUname = hUname
        self.hPwd = hPwd
        self.hKeyFile = hKeyFile
        self.iFaces = set()
        self.hosts = {}
        
    def addIface(self, ifname):
        self.ifaces.add(ifname)

    def load(self):
        nova = Client(self.VERSION, self.osUname, 
                      self.osPwd, self.osTenant, self.osUrl)
        self.hosts = nova.hosts.list()
        pass
        
    def hasElement(self, name):
        return (name in self.hosts)
    
    def getElementList(self):
        return self.hosts
    
    def getElementLinks(self, name):
        if not self.hasElement(name):
            return []
        
        ssh = paramiko.SSHClient()
        ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        ssh.connect(name, username=self.hUname, 
                    password=self.hPwd, key_filename=self.hKeyFile)
        stdin, stdout, stderr = ssh.exec_command("sudo lldpctl")
        stdin.write(self.hPwd)  # Send password for sudo
        stdin.flush()
        parseLldpCtl(stdout, self.hosts, name, self.iFaces)
        ssh.close()
        
        return self.hosts[name].values()


class SNMPSwitchLoader(NetDataSource):
    SNMP_PING = ("SNMPv2-MIB", "sysDescr", 0)
    LLDP_NEIGH = ('lldpMIB', "")
            
    def __init__(self, snmpCommunity, snmpPort):
        self.snmpCommunity = snmpCommunity
        self.snmpPort = snmpPort
        self.switches = {}
        self.snmpCmd = cmdgen.CommandGenerator()
        
    def snmpVar(self, key):
        return cmdgen.MibVariable(key[0], key[1], key[2]) # 

    def snmpGet(self, host, var):
        err, errStatus, errIndex, result = self.snmpCmd.getCmd(
            cmdgen.CommunityData(self.snmpCommunity),
            cmdgen.UdpTransportTarget((host, self.snmpPort)),
            var, lookupNames=True, lookupValues=True)
        return err, result

    def load(self):
        pass
        
    def hasElement(self, name):
        err, result = self.snmpVar(self.SNMP_PING)
        return not err
    
    def getElementList(self):
        return []
    
    def getElementLinks(self, name):
        links = []

        # Get LLDP neighbors
        neighbors = self.snmpGet()
        for linkmatch in links:
            new_link = Link(src=name, srcPort=normalizePortNum(linkmatch[0]),
                            dst=normalizeHostname(linkmatch[1]), 
                            dstPort=normalizePortNum(linkmatch[2]))

        # Get link speeds
        # Get port channels
        return self.switches[name].values()

class TopoGen:
    def __init__(self):
        self.hostDataSources = set()
        self.netDataSources = set()
        self.links = []
        self.hosts = []
        self.switches = []
        self.loaded = False
        
    def setHostLoader(self, netDataSource):
        self.hostDataSources.add(netDataSource)
    
    def setNetLoader(self, netDataSource):
        self.netDataSources.add(netDataSource)

    def loadSources(self):
        for hs in self.hostDataSources:
            hs.load()
        for ns in self.netDataSources:
            ns.load()
        self.loaded = True
            
    def topogen(self):
        self.hosts = []
        self.switches = []
        self.links = []
        queue = deque([])
        visited = set()

        if not self.loaded:
            self.loadSources()
                    
        for hs in self.hostDataSources:
            for host in hs.getElementList():
                queue.append((host, hs))
                self.hosts.append(host)
            
        while len(queue):
            (elem, ds) = queue.popleft()
            
            if elem in visited:
                continue
            
            if elem not in self.hosts:
                self.switches.append(elem)
            
            elem_links = ds.getElementLinks(elem)
            for link in elem_links:
                if link.dst not in visited:
                    self.links.append(link)
                    for source in self.netDataSources:
                        if source.hasElement(link.dst):
                            queue.append((link.dst,source))
                            
            visited.add(elem)

    def outJson(self, outfile):
        if outfile != "-":
            f = open(outfile, "w")
        else:
            f = sys.stdout
        f.write('[\n'+',\n'.join(map(str, self.links))+"\n]\n")

    def outGraphViz(self, outfile):
        if outfile != "-":
            f = open(outfile, "w")
        else:
            f = sys.stdout
        f.write("graph " + shortName(outfile) + "{\n")
        f.write("\trankdir=TB;\n")
        f.write('\tedge  [fontname="Arial" color="#cccccc" weight=1]\n')
        f.write('\t{\n\t\trank=same;')
        for host in self.hosts:
            f.write(shortName(host)+';')
        f.write('\n\t}\n')
        for host in self.hosts:
            f.write('\t' + shortName(host) + ' [style="rounded,filled,bold", ' +
                    'shape = box, fontname="Arial"];\n')
        for switch in self.switches:
            f.write('\t' + shortName(switch) + ' [shape = box]\n')
        for link in reversed(self.links):
            f.write('\t"' + shortName(link.dst) + '" -- "' + 
                    shortName(link.src) +'";\n')
        f.write("}\n")
        f.close()

def cli_main(argv=None):
    '''Command line options.'''

    prog_name = os.path.basename(sys.argv[0])
    prog_ver = "v0.1"
    prog_build_date = "%s" % __updated__

    ver_str = '%%prog %s (%s)' % (prog_ver, prog_build_date)
    desc = '''Generate physical network topology JSON file'''
    lic = "Copyright 2014 Kaustubh Joshi (AT&T Labs)                                            \
            Licensed under the Apache License 2.0\n\
            http://www.apache.org/licenses/LICENSE-2.0p;"

    if argv is None:
        argv = sys.argv[1:]

    parser = configargparse.ArgumentParser(epilog=desc, description=lic)
        
    # Allow reading config from a configuration file
    parser.add_argument("--config", is_config_file=True, help="Config file path")
    
    # Options for host lldpctl output parser
    parser.add_argument("-l", "--lldpctl", dest="lldpctl_file", action="append",
                        help="lldpctl output", metavar="FILE")

    # Options for switch CLI output parser
    parser.add_argument("-s", "--switchcli", dest="switch_file", action="append", 
                        help="switch CLI output", metavar="FILE")

    # Host options defaults
    parser.add_argument("--hostcap", dest="hostcap", metavar="bps",
                        help="host link capacity [default: %default]")
    parser.add_argument("-i", "--hostiface", dest="hostifaces", action='append',
                        help="host interfaces to include (e.g., eth2,eth3)")
    
    # Options for openstack loader
    parser.add_argument("--openstack", dest="ostack", action="store_true")
    parser.add_argument("--os_auth", dest="osUrl", 
                        help="keystone authentication URL", metavar="URL")
    parser.add_argument("--os_tenant_name", dest="osTenant", 
                        help="openstack tenant name")
    parser.add_argument("--os_username", dest="osUname", 
                        help="openstack username")
    parser.add_argument("--os_password", dest="osPwd", 
                        help="openstack password")
    parser.add_argument("--host_username", dest="hUname", 
                        help="compute/L3 host username")
    parser.add_argument("--host_password", dest="hPwd", 
                        help="compute/L3 host password")
    parser.add_argument("--host_keyfile", dest="hKeyFile", 
                        help="compute/L3 host ssh key file")

    # Options for switch SNMP loader
    parser.add_argument("--snmp", dest="snmp", action="store_true")    
    parser.add_argument("--community", dest="snmp_community", 
                        help="switch SNMP community", metavar="community_name")

    # General options
    parser.add_argument("-o", "--out", dest="outJsonFile", metavar="FILE",
                        help="set json output file [default: - (stdout)]")
    parser.add_argument("-g", "--graphviz", dest="outGvFile", metavar="FILE",
                        help="set graphviz output file")
    parser.add_argument("-v", "--verbose", dest="verbose", action="count", 
                        help="set verbosity level [default: %default]")

    parser.set_defaults(outJsonFile="-")
    parser.set_defaults(hostcap="1000000000")
    
    parser.set_defaults(ostack=False)
    parser.set_defaults(snmp=False)
    parser.set_defaults(osUrl=os.getenv('OS_AUTH_URL'))
    parser.set_defaults(osTenant=os.getenv('OS_TENANT_NAME'))
    parser.set_defaults(osUname=os.getenv('OS_USERNAME'))
    parser.set_defaults(osPwd=os.getenv('OS_PASSWORD'))

    # process options
    opts = parser.parse_args(argv)

    # MAIN BODY #
    topogen = TopoGen()

    DEF_HOSTCAPACITY = opts.hostcap
            
    if opts.lldpctl_file:
        for filename in opts.lldpctl_file:
            lldp_loader = LldpctlFileLoader(filename)
            for iface in opts.hostifaces:
                lldp_loader.addIface(iface)
            topogen.setHostLoader(lldp_loader)
    
    if opts.ostack: 
        openstack_loader = \
            OpenStackHostLoader(osUrl=opts.osUrl, osTenant=opts.osTenant,
                                osUname=opts.osUname, osPwd=opts.osPwd,
                                hUname=opts.hUname, hPwd=opts.hPwd,
                                hKeyFile=opts.hKeyFile)
        for iface in opts.hostifaces:
            lldp_loader.addIface(iface)
        topogen.setHostLoader(openstack_loader)

    if opts.switch_file:
        for filename in opts.switch_file:
            switch_loader = SwitchFileLoader(filename)
            topogen.setNetLoader(switch_loader)
            
    if opts.snmp_community:
        snmp_loader = SNMPSwitchLoader(opts.snmp_community)
        topogen.setNetLoader(snmp_loader)
           
    # Generate the topology 
    topogen.topogen()
    
    # Now output json and optionally, graphviz
    if opts.outJsonFile:
        topogen.outJson(opts.outJsonFile)
        
    if opts.outGvFile:
        topogen.outGraphViz(opts.outGvFile)

if __name__ == "__main__":
    sys.exit(cli_main())
