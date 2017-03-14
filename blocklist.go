package main

import (
  "fmt"
  "net"
  "bufio"
  "strings"
  "net/http"
  "time"
  "runtime"
  "log/syslog"
  "log"
)

type IPSet struct {
  root BitNode
}


type BitNode struct {
  parent,zero,one *BitNode
  depth uint32
  full bool
  value uint32
}

func main () {

  
  ipsetCurrent := createIPSet()
  ipsetNew := createIPSet()

  blocklists := []string{ "https://www.confusticate.com/all.txt",
    "https://www.confusticate.com/drop.txt",
    "https://www.confusticate.com/edrop.txt"}
    
  sysLog, err := syslog.Dial("udp", "localhost:514",
		syslog.LOG_INFO|syslog.LOG_DAEMON, "blocklist")
	if err != nil {
		log.Fatal(err)
	}
  

  for 1==1 {
    var announcements = 0
    var withdrawls = 0
    
    sysLog.Info("begin blocklist refresh")

    for _, url := range blocklists {

      data := downloadBlocklist(url)

      sysLog.Info(fmt.Sprintf("%d entries downloaded from %s\n",len(data), url))

      for _, ipnet := range data {
        ipsetNew.add(&ipnet)
      }
    }

    // If IPs in the new set aren't in the current set, announce them
    for _, newip := range ipsetNew.getAll() {
      current := ipsetCurrent.contains(&newip)
      if current == nil {
        fmt.Printf("announce route %s next-hop 192.0.2.1 community [65332:666]\n", newip.String())
        announcements++
      } else {
        // However, if the thing that matched wasn't identical to the existing one 
        // I.e. more specific IP, withdraw the existing one *and* annouce the new one
        if current.String() != newip.String() {
          fmt.Printf("withdraw route %s next-hop 192.0.2.1 community [65332:666]\n", current.String())
          fmt.Printf("announce route %s next-hop 192.0.2.1 community [65332:666]\n", newip.String())
          announcements++
          withdrawls++
        }  
      }    
    }
    
    // If IPs from the current set aren't in the new set
    for _, existing := range ipsetCurrent.getAll() {
      newip := ipsetNew.contains(&existing)
      
      if newip == nil {
        fmt.Printf("withdraw route %s next-hop 192.0.2.1 community [65332:666]\n", existing.String())
        withdrawls++
      } else {      
        // However, if the thing that matched wasn't identical to the existing one 
        // I.e. more specific IP, withdraw the existing one *and* annouce the new one
        if newip != nil && newip.String() != existing.String() {
          fmt.Printf("withdraw route %s next-hop 192.0.2.1 community [65332:666]\n", existing.String())
          fmt.Printf("announce route %s next-hop 192.0.2.1 community [65332:666]\n", newip.String())
          announcements++
          withdrawls++
        }
      }
    }

    ipsetCurrent = ipsetNew
    ipsetNew = createIPSet()
    sysLog.Info(fmt.Sprintf("completed with %d routes announced and %d routes withdrawn\n",announcements, withdrawls))
    runtime.GC()
    time.Sleep(30 * time.Minute)
  }
}

func downloadBlocklist(url string) []net.IPNet {
  var nets = make([]net.IPNet,0)
  resp, err := http.Get(url)
  
  if err != nil {
    fmt.Println(err)
    return nil
  }
  
  scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
    line := scanner.Text()
    items := strings.Split(line,";")
    ipnet := StringToIPNet(items[0])

    if ipnet != nil {
        nets = append(nets,*ipnet)
    }
	}
  
  
  return nets
}


// ====================================================================================================================

func createIPSet() IPSet {
  return IPSet { root:  BitNode{parent: nil, zero :nil, one :nil, depth: 32, full: false, value: 0} }
}


func (ipset *IPSet) add(ipnet *net.IPNet) {
  if ipnet != nil {
      var bits,_ = ipnet.Mask.Size()
      var addr = IPtoInt(ipnet.IP)

      addIP(&ipset.root,addr,uint32(bits))
  }
}

func (ipset *IPSet) getAll() []net.IPNet {
  return collectIPs(&ipset.root)
}

func (ipset *IPSet) contains(ipnet *net.IPNet) *net.IPNet {
  if ipnet != nil {
      var bits,_ = ipnet.Mask.Size()
      var addr = IPtoInt(ipnet.IP)

      return containsIP(&ipset.root,addr,uint32(bits))
  } else {
    return nil
  }
}


func StringToIPNet(text string) *net.IPNet {
  var ip,ipnet,error = net.ParseCIDR(strings.TrimSpace(text))

  if error != nil {
    ip = net.ParseIP(text)
    if ip != nil {
      ipnet = &net.IPNet{IP: ip, Mask: net.CIDRMask(32,32)}
    }
  }
  
  return ipnet
}

func IPtoInt(addr net.IP) uint32 {
  return uint32(addr.To4()[0]) << 24 |
         uint32(addr.To4()[1]) << 16 |
         uint32(addr.To4()[2]) << 8 |
         uint32(addr.To4()[3])
}

func IntToIP(ip uint32) net.IP {
  result := make(net.IP, 4)
           result[3] = byte(ip)
           result[2] = byte(ip >>8)
           result[1] = byte(ip >>16)
           result[0] = byte(ip >> 24)
  return result
}

func CheckBit(num uint32, bit uint32) bool {
  return (num & (1 << bit)) != 0
}


func outputTree(node *BitNode) {
  if node.full {
    var ipnet = IPNetFromNode(node)
    fmt.Println(ipnet.String())
  } else {
    if node.zero != nil {
      outputTree(node.zero)
    }
  
    if node.one != nil {
      outputTree(node.one)
    }
  }
}

func collectIPs(node *BitNode) []net.IPNet {
  var nets = make([]net.IPNet,0)
  
  if node.full {
    var ipnet = IPNetFromNode(node)
    return []net.IPNet{ipnet}
  } else {
    if node.zero != nil {
      nets = append(nets,collectIPs(node.zero)...)
    }
  
    if node.one != nil {
      nets = append(nets,collectIPs(node.one)...)
    }
  }
  return nets
}


func IPFromNode(node *BitNode) net.IP {
  var cur = node
  var accumulate uint32 = 0
  
  for cur.parent != nil {
    accumulate |= cur.value << cur.depth
    cur = cur.parent
  }
  
  return IntToIP(accumulate)
}

func IPNetFromNode(node *BitNode) net.IPNet {
  var cur = node
  var accumulate uint32 = 0
  var mask = int(32 - node.depth)
  
  for cur.parent != nil {
    accumulate |= cur.value << cur.depth
    cur = cur.parent
  }
  
  return net.IPNet{IP: IntToIP(accumulate), Mask: net.CIDRMask(mask,32)}
}


func containsIP(node *BitNode, addr uint32, mask uint32) *net.IPNet {
  if node.full {
    ipnet := IPNetFromNode(node)
    
    return &ipnet
  }
  
  if 32-node.depth > mask {
    return nil
  }
    
  if CheckBit(addr,node.depth - 1) {
    if node.one == nil {
      return nil
    } else {
      return containsIP(node.one,addr,mask)
    }
  } else {
    if node.zero == nil {
      return nil
    } else {
      return containsIP(node.zero,addr,mask)
    }
  }
}


func addIP(node *BitNode, addr uint32, mask uint32) bool {

  if node.depth == 0 || 32 - node.depth == mask || node.full {
    node.full = true
    return node.full
  }
  
  var child *BitNode
    
  if CheckBit(addr,node.depth - 1) {
    if node.one == nil {
      child = &BitNode{parent: node, zero: nil, one: nil, depth: node.depth -1, full: false, value: 1}    
      node.one = child
    } else {
      child = node.one
    }
  } else {
    if node.zero == nil {
      child = &BitNode{parent: node, zero: nil, one: nil, depth: node.depth -1, full: false, value: 0 }    
      node.zero = child
    } else {
      child = node.zero
    }
  }
  
  addIP(child, addr, mask)
  
  node.full = (node.one != nil && node.one.full) && (node.zero != nil && node.zero.full) 
  return node.full
}
