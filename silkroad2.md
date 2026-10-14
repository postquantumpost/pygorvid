    36.3. [ ] Re-run the relevant Go/Python/Rust regression checks for each ported increment before broadening the supported H.264 feature union.
        36.4. [ ] Add default-off per-frame progress reporting to Rust's full-resolution decoder tests, emitting 0%-100% in 10% increments to live stderr and the test log; replace the unconditional macroblock-specific CABAC diagnostics.
        36.5. [ ] Once Go has a complete frame-decode test path, add matching default-off per-frame progress reporting for full-resolution tests.
        36.6. [ ] Investigate whether decoder-scoped SPS/PPS parse caching benefits Go and Rust; profile repeated parameter-set parsing on their full-frame paths before porting.
        36.7. [ ] Investigate Rust equivalents of Python's immutable reference-plane validation fast path, no-op deblocking edge skip, and residual-context range validation; profile before porting.
        36.8. [ ] Investigate Go equivalents of Python's immutable reference-plane validation fast path, no-op deblocking edge skip, and residual-context range validation; profile before porting.
        36.9. [ ] In timeout-enabled luma interpolation, retain chunk-level deadline polls but skip repeated sample-range scans for immutable `bytes` reference planes.
        36.10. [ ] Return an existing packed Y/U/V plane when its stride equals its width instead of copying rows into a new byte string.
        36.11. [ ] Profile and cache reusable quarter-sample neighborhoods or interpolation tiles across adjacent output pixels and macroblocks.
        36.12. [ ] Batch eligible deblocking edge segments per macroblock and use a validated internal fast path while preserving normative edge order.
        36.13. [ ] Profile and cache deblocking thresholds and QP-derived parameters shared by adjacent edges.
        36.14. [ ] Precompute reusable boundary-strength and neighbor-prediction facts rather than deriving them repeatedly during deblocking.
        36.15. [ ] Profile CABAC transaction cloning after selective residual-context cloning; evaluate a mutation journal/checkpoint rollback path with transactional regressions.
        36.16. [ ] Fuse luma 4x4 inverse scaling and inverse transform where vectors confirm identical rounding and output.
        36.17. [ ] Reduce repeated validation and type checks inside trusted pixel/deblocking loops while retaining strict public-boundary checks.
        36.18. [ ] Profile and reduce temporary per-macroblock prediction lists and repeated full-plane packing/copying.
