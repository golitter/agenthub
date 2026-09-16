from pathlib import Path
scope = {}
exec(Path('/workspace/solution.py').read_text(encoding='utf-8'), scope)
assert scope['TASK_ID'] == 'feature-core-003'
assert scope['STATUS'] == 'IMPLEMENTED'
print('1 passed')
