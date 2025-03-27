package payload

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateNetworkConfigScript(t *testing.T) {
	expectedOut := `# This script ensures the contents of the CNI config file is correct, and creates the kube-proxy config file.

param(
    [string]$hostnameOverride,
    [string]$clusterCIDR,
    [string]$kubeConfigPath,
    [string]$kubeProxyConfigPath,
    [string]$verbosity
)
  # this compares the config with the existing config, and replaces if necessary
  function Compare-And-Replace-Config {
    param (
        [string]$ConfigPath,
        [string]$NewConfigContent
    )
    
    # Read existing config content
    $existing_config = ""
    if (Test-Path -Path $ConfigPath) {
        $config_file_content = Get-Content -Path $ConfigPath -Raw
        if ($config_file_content -ne $null) {
` + "        $existing_config=$config_file_content.Replace(\"`r\",\"\")" + `
        }
    }
    
    if ($existing_config -ne $NewConfigContent) {
        Set-Content -Path $ConfigPath -Value $NewConfigContent -NoNewline
    }
  }

$ErrorActionPreference = "Stop"
Import-Module -DisableNameChecking c:\k\hns.psm1

$cni_template=@'
{
    "cniVersion":"0.2.0",
    "name":"OVNKubernetesHNSNetwork",
    "type":"win-overlay",
    "apiVersion": 2,
    "capabilities":{
        "portMappings": true,
        "dns":true
    },
    "ipam":{
        "type":"host-local",
        "subnet":"ovn_host_subnet"
    },
    "policies":[
    {
        "name": "EndpointPolicy",
        "value": {
            "type": "OutBoundNAT",
            "settings": {
                "exceptionList": [
                "10.0.0.1/32"
                ],
                "destinationPrefix": "",
                "needEncap": false
            }
        }
    },
    {
        "name": "EndpointPolicy",
        "value": {
            "type": "SDNRoute",
            "settings": {
                "exceptionList": [],
                "destinationPrefix": "10.0.0.1/32",
                "needEncap": true
            }
        }
    },
    {
        "name": "EndpointPolicy",
        "value": {
            "type": "ProviderAddress",
            "settings": {
                "providerAddress": "provider_address"
            }
        }
    }
    ]
}
'@

# Generate CNI Config
$hns_network=Get-HnsNetwork  | where { $_.Name -eq 'OVNKubernetesHNSNetwork'}
$subnet=$hns_network.Subnets.AddressPrefix
$cni_template=$cni_template.Replace("ovn_host_subnet",$subnet)
$provider_address=$hns_network.ManagementIP
$cni_template=$cni_template.Replace("provider_address",$provider_address)

Compare-And-Replace-Config -ConfigPath c:\k\cni.conf -NewConfigContent $cni_template

# Create HNS endpoint if it doesn't exist
$endpoint = Invoke-HNSRequest GET endpoints | where { $_.Name -eq 'VIPEndpoint'}
if( $endpoint -eq $null) {
    $endpoint = New-HnsEndpoint -NetworkId $hns_network.ID -Name "VIPEndpoint"
    Attach-HNSHostEndpoint -EndpointID $endpoint.ID -CompartmentID 1
}
# Get HNS endpoint IP
$sourceVip = (Get-NetIPConfiguration -AllCompartments -All -Detailed | where { $_.NetAdapter.LinkLayerAddress -eq $endpoint.MacAddress }).IPV4Address.IPAddress.Trim()

#Kube Proxy configuration

$kube_proxy_config=@"
kind: KubeProxyConfiguration
apiVersion: kubeproxy.config.k8s.io/v1alpha1
featureGates:
  WinDSR: true
  WinOverlay: true
clientConnection:
  kubeconfig: $kubeConfigPath
  acceptContentTypes: ''
  contentType: ''
  qps: 0
  burst: 0
logging:
  flushFrequency: 0
  verbosity: $verbosity
  options:
    text:
      infoBufferSize: '0'
    json:
      infoBufferSize: '0'
hostnameOverride: $hostnameOverride
bindAddress: ''
healthzBindAddress: ''
metricsBindAddress: ''
bindAddressHardFail: false
enableProfiling: false
showHiddenMetricsForVersion: ''
mode: kernelspace
iptables:
  masqueradeBit: null
  masqueradeAll: false
  localhostNodePorts: null
  syncPeriod: 0s
  minSyncPeriod: 0s
ipvs:
  syncPeriod: 0s
  minSyncPeriod: 0s
  scheduler: ''
  excludeCIDRs: null
  strictARP: false
  tcpTimeout: 0s
  tcpFinTimeout: 0s
  udpTimeout: 0s
nftables:
  masqueradeBit: null
  masqueradeAll: false
  syncPeriod: 0s
  minSyncPeriod: 0s
winkernel:
  networkName: OVNKubernetesHybridOverlayNetwork
  sourceVip: $sourceVip
  enableDSR: true
  rootHnsEndpointName: ''
  forwardHealthCheckVip: false
detectLocalMode: ''
detectLocal:
  bridgeInterface: ''
  interfaceNamePrefix: ''
clusterCIDR: $clusterCIDR
nodePortAddresses: null
oomScoreAdj: null
conntrack:
  maxPerCore: null
  min: null
  tcpEstablishedTimeout: null
  tcpCloseWaitTimeout: null
  tcpBeLiberal: false
  udpTimeout: 0s
  udpStreamTimeout: 0s
configSyncPeriod: 0s
portRange: ''
"@

# Generate kube-proxy config 
Compare-And-Replace-Config -ConfigPath $kubeProxyConfigPath -NewConfigContent $kube_proxy_config
`
	actual, err := generateNetworkConfigScript("10.0.0.1/32",
		"OVNKubernetesHNSNetwork", "c:\\k\\hns.psm1", "c:\\k\\cni.conf")
	require.NoError(t, err)
	assert.Equal(t, string(expectedOut), actual)
}

func TestReplaceContainerdSandboxImage(t *testing.T) {
	inputConfig := `
[plugins]

  [plugins."io.containerd.grpc.v1.cri"]
    cdi_spec_dirs = []
    sandbox_image = "mcr.microsoft.com/oss/kubernetes/pause:3.9"
    tolerate_missing_hugetlb_controller = false
    unset_seccomp_profile = ""
`
	pauseImage := "my-registry.test/pause:3.9"
	expected := `
[plugins]

  [plugins."io.containerd.grpc.v1.cri"]
    cdi_spec_dirs = []
    sandbox_image = "my-registry.test/pause:3.9"
    tolerate_missing_hugetlb_controller = false
    unset_seccomp_profile = ""
`
	actual, err := replaceContainerdSandboxImage(inputConfig, pauseImage)
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}
