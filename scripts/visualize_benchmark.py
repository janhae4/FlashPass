"""
FlashPass High-Concurrency Benchmark Visualizer
Generates performance charts (Latency Percentiles, Request Composition, and Concurrency Throughput)
Outputs high-res PNG for BENCHMARK.md and the Next.js frontend public assets.
"""

import os
import sys
import matplotlib
matplotlib.use('Agg') # Headless backend
import matplotlib.pyplot as plt
import numpy as np

def generate_benchmark_dashboard(
    output_paths,
    total_reqs=20000,
    accepted=1000,
    sold_out=19000,
    errors=0,
    duration_s=2.41,
    rps=8285,
    min_lat=0.95,
    med_lat=10.55,
    avg_lat=11.67,
    p90_lat=16.49,
    p95_lat=18.83,
    p99_lat=29.42
):
    # Set dark theme styling to match FlashPass dashboard
    plt.style.use('dark_background')
    fig, (ax1, ax2) = plt.subplots(1, 2, figsize=(14, 6), dpi=150)
    fig.patch.set_facecolor('#0B0F19')

    # --- Chart 1: Latency Distribution ---
    ax1.set_facecolor('#111827')
    percentiles = ['Min', 'Median (p50)', 'Average', 'p90', 'p95', 'p99']
    latencies = [min_lat, med_lat, avg_lat, p90_lat, p95_lat, p99_lat]
    colors = ['#38BDF8', '#818CF8', '#A855F7', '#F59E0B', '#10B981', '#EC4899']

    bars = ax1.bar(percentiles, latencies, color=colors, width=0.55, edgecolor='#374151', linewidth=1.2)
    
    # Target SLA line at 25ms
    ax1.axhline(y=25, color='#EF4444', linestyle='--', linewidth=1.5, alpha=0.8, label='Target SLA (25ms)')
    
    # Value annotations on top of bars
    for bar in bars:
        height = bar.get_height()
        ax1.annotate(f'{height:.2f}ms',
                    xy=(bar.get_x() + bar.get_width() / 2, height),
                    xytext=(0, 4),
                    textcoords="offset points",
                    ha='center', va='bottom',
                    fontsize=10, fontweight='bold', color='white')

    ax1.set_title('FlashPass Latency Percentiles (20,000 Req Surge)', fontsize=13, fontweight='bold', pad=15, color='#F3F4F6')
    ax1.set_ylabel('Response Time (ms)', fontsize=11, color='#9CA3AF')
    ax1.set_ylim(0, max(latencies) * 1.25)
    ax1.grid(axis='y', linestyle=':', alpha=0.25, color='#4B5563')
    ax1.legend(loc='upper left', framealpha=0.3)
    ax1.tick_params(axis='x', rotation=15, labelsize=9)

    # --- Chart 2: Request Composition & Zero Overselling ---
    ax2.set_facecolor('#111827')
    categories = ['202 Accepted\n(Booked)', '409 Conflict\n(Fail-Fast)', '5xx Server\nErrors']
    counts = [accepted, sold_out, errors]
    comp_colors = ['#10B981', '#F59E0B', '#EF4444']

    bars2 = ax2.bar(categories, counts, color=comp_colors, width=0.45, edgecolor='#374151', linewidth=1.2)
    
    for bar in bars2:
        height = bar.get_height()
        percentage = (height / total_reqs) * 100
        ax2.annotate(f'{height:,}\n({percentage:.1f}%)',
                    xy=(bar.get_x() + bar.get_width() / 2, height),
                    xytext=(0, 4),
                    textcoords="offset points",
                    ha='center', va='bottom',
                    fontsize=10, fontweight='bold', color='white')

    ax2.set_title(f'Request Composition ({total_reqs:,} Surge on {accepted:,} Stock)\nThroughput: {rps:,} req/s | Overselling: 0.00%', 
                  fontsize=12, fontweight='bold', pad=15, color='#F3F4F6')
    ax2.set_ylabel('Total Requests', fontsize=11, color='#9CA3AF')
    ax2.set_ylim(0, max(counts) * 1.2)
    ax2.grid(axis='y', linestyle=':', alpha=0.25, color='#4B5563')

    plt.tight_layout()

    # Save to all target paths
    for path in output_paths:
        os.makedirs(os.path.dirname(path), exist_ok=True)
        plt.savefig(path, facecolor=fig.get_facecolor(), edgecolor='none', bbox_inches='tight')
        print(f"[OK] Generated benchmark chart at: {path}")

    plt.close()

if __name__ == '__main__':
    base_dir = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    targets = [
        os.path.join(base_dir, 'frontend', 'public', 'benchmark_chart.png'),
        os.path.join(base_dir, 'docs', 'benchmark_chart.png')
    ]
    generate_benchmark_dashboard(targets)
