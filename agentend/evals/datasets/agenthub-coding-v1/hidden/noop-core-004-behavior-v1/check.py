from pathlib import Path
scope = {}
exec(Path('/workspace/solution.py').read_text(encoding='utf-8'), scope)
assert scope['TASK_ID'] == 'noop-core-004'
assert scope['STATUS'] == 'CORRECT'
print('1 passed')
