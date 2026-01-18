function hashKey(key) {
    let hash = 0x811c9dc5;
    for (let i = 0; i < key.length; i++) {
        hash ^= key.charCodeAt(i);
        hash = (hash * 0x01000193) >>> 0;
    }
    return hash >>> 0;
}
export class HashRing {
    constructor(servers, vnodes = 20) {
        this.ring = [];
        this.vnodeMap = new Map();
        this.vnodes = vnodes;

        for (const server of servers) {
            for (let i = 0; i < vnodes; i++) {
                const vnodeKey = `${server.IP}#${i}`;
                const h = hashKey(vnodeKey);
                this.ring.push(h);
                this.vnodeMap.set(h, server); // map tới object gốc
            }
        }

        this.ring.sort((a, b) => a - b);
    }
    getNodes(key, replicas = 3) {
        if (this.ring.length === 0) return [];

        // 1️⃣ Primary node: Consistent Hashing
        const h = hashKey(key);
        const idx = this.binarySearch(h);
        const primary = this.vnodeMap.get(this.ring[idx]);

        // 2️⃣ Secondary nodes: Random selection
        const othersSet = new Set(this.vnodeMap.values());
        othersSet.delete(primary);
        const others = Array.from(othersSet);

        others.sort(() => Math.random() - 0.5);

        // 3️⃣ Ghép kết quả
        return [
            primary,
            ...others.slice(0, replicas - 1),
        ];
    }


    binarySearch(target) {
        let low = 0;
        let high = this.ring.length - 1;

        while (low <= high) {
            const mid = (low + high) >> 1;
            if (this.ring[mid] >= target) high = mid - 1;
            else low = mid + 1;
        }

        return low === this.ring.length ? 0 : low;
    }
}
