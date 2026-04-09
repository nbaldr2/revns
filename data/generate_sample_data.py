#!/usr/bin/env python3
"""
Sample data generation script for REVNS testing.
Generates CSV data with domain names, nameservers, and ranks.
"""

import csv
import random
import argparse
from typing import List

# Sample TLDs
TLDS = ['com', 'org', 'net', 'io', 'co', 'app', 'dev', 'cloud']

# Sample nameserver domains
NS_DOMAINS = [
    'ns1.cloudflare.com',
    'ns2.cloudflare.com',
    'ns1.google.com',
    'ns2.google.com',
    'ns1.awsdns.com',
    'ns2.awsdns.com',
    'ns1.godaddy.com',
    'ns2.godaddy.com',
    'ns1.bluehost.com',
    'ns2.bluehost.com',
    'ns1.hostgator.com',
    'ns2.hostgator.com',
    'ns1.digitalocean.com',
    'ns2.digitalocean.com',
    'ns1.linode.com',
    'ns2.linode.com',
    'ns1.vultr.com',
    'ns2.vultr.com',
]

# Common words for domain generation
WORDS = [
    'tech', 'app', 'web', 'site', 'online', 'digital', 'cloud', 'data',
    'smart', 'pro', 'biz', 'hub', 'center', 'world', 'global', 'local',
    'fast', 'quick', 'secure', 'safe', 'easy', 'simple', 'modern', 'new',
    'best', 'top', 'super', 'mega', 'ultra', 'hyper', 'eco', 'green',
    'blue', 'red', 'green', 'gold', 'silver', 'black', 'white',
    'alpha', 'beta', 'gamma', 'delta', 'omega', 'prime', 'max', 'plus',
    'shop', 'store', 'market', 'mall', 'buy', 'sell', 'trade', 'deal',
    'news', 'blog', 'media', 'press', 'info', 'guide', 'help', 'support',
    'game', 'play', 'fun', 'sport', 'art', 'music', 'video', 'photo',
]


def generate_domain_name() -> str:
    """Generate a random domain name."""
    patterns = [
        lambda: f"{random.choice(WORDS)}{random.choice(WORDS)}",
        lambda: f"{random.choice(WORDS)}{random.randint(1, 999)}",
        lambda: f"{random.choice(WORDS)}-{random.choice(WORDS)}",
        lambda: f"{random.choice(WORDS)}{random.choice(WORDS)}{random.randint(1, 99)}",
        lambda: f"my{random.choice(WORDS)}",
        lambda: f"get{random.choice(WORDS)}",
        lambda: f"{random.choice(WORDS)}ly",
        lambda: f"the{random.choice(WORDS)}",
    ]
    name = random.choice(patterns)()
    tld = random.choice(TLDS)
    return f"{name}.{tld}"


def generate_nameservers() -> List[str]:
    """Generate 1-3 nameservers for a domain."""
    num_ns = random.choices([1, 2, 3], weights=[10, 60, 30])[0]
    return random.sample(NS_DOMAINS, num_ns)


def generate_csv(output_file: str, num_records: int):
    """Generate CSV file with sample data."""
    with open(output_file, 'w', newline='') as f:
        writer = csv.writer(f)
        writer.writerow(['domain', 'ns_list', 'rank'])
        
        used_domains = set()
        for rank in range(1, num_records + 1):
            # Ensure unique domains
            while True:
                domain = generate_domain_name()
                if domain not in used_domains:
                    used_domains.add(domain)
                    break
            
            ns_list = generate_nameservers()
            writer.writerow([domain, '|'.join(ns_list), rank])
            
            if rank % 10000 == 0:
                print(f"Generated {rank} records...")
    
    print(f"Generated {num_records} records in {output_file}")
    
    # Print nameserver distribution summary
    print("\nNameserver distribution:")
    ns_counts = {}
    with open(output_file, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            for ns in row['ns_list'].split('|'):
                ns_counts[ns] = ns_counts.get(ns, 0) + 1
    
    for ns, count in sorted(ns_counts.items(), key=lambda x: x[1], reverse=True)[:10]:
        print(f"  {ns}: {count} domains")


def main():
    parser = argparse.ArgumentParser(description='Generate sample domain data')
    parser.add_argument('-o', '--output', default='domains.csv', help='Output CSV file')
    parser.add_argument('-n', '--num-records', type=int, default=100000, 
                        help='Number of records to generate (default: 100000)')
    args = parser.parse_args()
    
    generate_csv(args.output, args.num_records)


if __name__ == '__main__':
    main()
