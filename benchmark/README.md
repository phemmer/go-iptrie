This package provides a benchmark for the different IP tree implementations.  
Even though it's part of the go-iptrie package, it aims to be unbiased and representative. If you feel a test does not accurately represent expected performance, please feel free to adjust the test, or add a new one.

# Tests
All tests are deterministic, which each run & package using the same data.

The read tests are validated to ensure each package produces the same results. If any package does not support the test, or produces a different result, it will be marked `N/A` in the results table.

## LoadNets Random
This test loads a large number of random networks into the tree, with each entry containing associated data. Many of the subnets will overlap.

## LoadNets Sorted
This test sorts the networks before performing the same operation as 'LoadNets Random'.

## Read Check
This test checks a large number of random addresses for presence in the tree. The tree is loaded using the same networks as the LoadNets tests. At least 10% of the addresses are guaranteed to be matches.

## Read Lookup
This test retrieves the value stored with the network that matches each of a large number of random addresses. The tree is loaded using the same networks as the LoadNets tests. At least 10% of the addresses are guaranteed to be matches.

# Packages

| Name     | Package                                                                                               | Repo                                      |
|----------|-------------------------------------------------------------------------------------------------------|-------------------------------------------|
| IPTrie   | [github.com/phemmer/go-iptrie](https://pkg.go.dev/github.com/phemmer/go-iptrie)                       | https://www.github.com/phemmer/go-iptrie  |
| Infoblox | [github.com/infobloxopen/go-trees/iptree](https://pkg.go.dev/github.com/infobloxopen/go-trees/iptree) | https://github.com/infobloxopen/go-trees/ |
| NRadix   | [github.com/asergeyev/nradix](https://pkg.go.dev/github.com/asergeyev/nradix)                         | https://github.com/asergeyev/nradix       |
| Ranger   | [github.com/yl2chen/cidranger](https://pkg.go.dev/github.com/yl2chen/cidranger)                       | https://github.com/yl2chen/cidranger/     |

# Results
These results measure the performance of each test. The value is the number of operations per second, with the percentage compared to the fastest result in parentheses.

|   *(OPs/Sec)*   | IPTrie           |     Infoblox      |      NRadix       |      Ranger       |
|-----------------|---------------------|-------------------|-------------------|-------------------|
| LoadNets Random | 187,028 (43.0%)     | 157,774 (36.3%)   | 434,458 (100.0%)  | 12,856 (3.0%)     |
| LoadNets Sorted | 500,611 (88.6%)     | 198,723 (35.2%)   | 564,891 (100.0%)  | 18,555 (3.3%)     |
| Read Check      | 10,757,118 (100.0%) | 2,335,254 (21.7%) | 4,818,485 (44.8%) | 7,099,999 (66.0%) |
| Read Lookup     | 2,680,126 (100.0%)  | 2,661,177 (99.3%) | N/A               | 439,872 (16.4%)   |
